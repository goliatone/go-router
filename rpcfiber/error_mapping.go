package rpcfiber

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	cmdrpc "github.com/goliatone/go-command/rpc"
	"github.com/goliatone/go-router"
)

// FailureStage identifies the RPC boundary that failed. It is diagnostic metadata,
// not a public error code or an indication that command effects were rolled back.
type FailureStage string

const (
	FailureStageParse        FailureStage = "parse"
	FailureStageResolve      FailureStage = "resolve"
	FailureStageDecode       FailureStage = "decode"
	FailureStageMetadata     FailureStage = "metadata"
	FailureStageBeforeInvoke FailureStage = "before_invoke"
	FailureStageInvoke       FailureStage = "invoke"
	FailureStageAfterInvoke  FailureStage = "after_invoke"
	FailureStageMapping      FailureStage = "mapping"
)

// FailureEvent is diagnostic-only. Err and Panic may contain private data and
// must never be copied into public responses. Method is client supplied.
type FailureEvent struct {
	Stage  FailureStage
	Method string
	Err    error
	Panic  any
}

// ErrorObserver observes each failed stage once, including failures before
// invocation. A failing invocation and a failing diagnostic/mapping callback may
// produce separate events. Observer panics are contained and are not re-reported.
type ErrorObserver func(router.Context, FailureEvent)

// WithErrorMapper opts into structured application errors for returned invocation
// and pre-invoke errors. Use cmdrpc.DefaultErrorMapper for the default safe
// classifier. A mapper returning nil declines to that classifier; a nil option
// restores legacy mode. Transport-stage codes and HTTP statuses are unchanged.
func WithErrorMapper(mapper cmdrpc.ErrorMapper) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.ErrorMapper = mapper
		}
	}
}

// WithErrorObserver installs a diagnostic observer without changing AfterInvoke's
// invocation-only semantics. The observer must not write a response.
func WithErrorObserver(observer ErrorObserver) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.ErrorObserver = observer
		}
	}
}

type rpcFailure struct {
	event  FailureEvent
	status int
	public cmdrpc.Error
}

func newRPCFailure(stage FailureStage, method string, err error, panicValue any) *rpcFailure {
	f := &rpcFailure{event: FailureEvent{Stage: stage, Method: method, Err: err, Panic: panicValue}, status: http.StatusBadRequest}
	switch stage {
	case FailureStageParse:
		f.public.Code, f.public.Message = "RPC_INVALID_REQUEST", "invalid RPC request payload"
	case FailureStageResolve:
		if method == "" {
			f.public.Code, f.public.Message = "RPC_METHOD_REQUIRED", "rpc method required"
		} else {
			f.status = http.StatusNotFound
			f.public.Code, f.public.Message = "RPC_METHOD_NOT_FOUND", fmt.Sprintf("rpc method %q not found", method)
		}
	case FailureStageDecode:
		f.public.Code, f.public.Message = "RPC_INVALID_PARAMS", "invalid rpc params"
	case FailureStageMetadata, FailureStageBeforeInvoke:
		f.public.Code, f.public.Message = "RPC_PREINVOKE_REJECTED", "rpc pre-invoke hook rejected request"
	default:
		f.status = http.StatusOK
		f.public.Code, f.public.Message = "RPC_INVOKE_FAILED", "rpc invocation failed"
		f.public.Details = map[string]any{"method": method}
	}
	return f
}

// panicFailure keeps error-valued panics available to errors.Is/As without
// formatting arbitrary panic values while handling a failure.
type panicFailure struct{ value any }

func (*panicFailure) Error() string   { return "rpc boundary panic" }
func (p *panicFailure) Unwrap() error { err, _ := p.value.(error); return err }

func (h mountHandler) observe(ctx router.Context, event FailureEvent) {
	if h.cfg.ErrorObserver == nil {
		return
	}
	defer func() { _ = recover() }()
	h.cfg.ErrorObserver(ctx, event)
}

func (h mountHandler) afterInvoke(ctx router.Context, method string, result any, err error) {
	if h.cfg.AfterInvoke == nil {
		return
	}
	defer func() {
		if p := recover(); p != nil {
			h.observe(ctx, FailureEvent{Stage: FailureStageAfterInvoke, Method: method, Err: &panicFailure{p}, Panic: p})
		}
	}()
	h.cfg.AfterInvoke(ctx, method, result, err)
}

func (h mountHandler) writeFailure(ctx router.Context, f *rpcFailure) error {
	h.observe(ctx, f.event)
	public := &f.public
	if h.cfg.ErrorMapper != nil && (f.event.Stage == FailureStageBeforeInvoke || f.event.Stage == FailureStageInvoke || f.event.Stage == FailureStageMetadata) {
		// A recovered panic is an internal failure, even if the panic value happens
		// to implement a domain-error interface. Keep that value diagnostic-only.
		if f.event.Panic != nil {
			public = cmdrpc.DefaultErrorMapper(ctx.Context(), errors.New("rpc boundary panic"))
		} else {
			encoded, err, panicValue := marshalMappedError(ctx.Context(), f.event.Err, h.cfg.ErrorMapper)
			if err == nil {
				return ctx.JSON(f.status, json.RawMessage(encoded))
			}
			h.observe(ctx, FailureEvent{Stage: FailureStageMapping, Method: f.event.Method, Err: err, Panic: panicValue})
			public = cmdrpc.DefaultErrorMapper(ctx.Context(), f.event.Err)
		}
	}
	return ctx.JSON(f.status, cmdrpc.ResponseEnvelope[any]{Error: public})
}

// Marshal exactly once under recovery so malformed public projections (cycles,
// unsupported details or custom marshalers that panic) cannot escape the mount.
// Returning RawMessage avoids calling a host Marshaler again during the write.
func marshalMappedError(ctx context.Context, err error, mapper cmdrpc.ErrorMapper) (raw []byte, mappingErr error, panicValue any) {
	defer func() {
		if p := recover(); p != nil {
			raw = nil
			mappingErr = &panicFailure{p}
			panicValue = p
		}
	}()
	public := cmdrpc.MapError(ctx, err, mapper)
	if public == nil || strings.TrimSpace(public.Code) == "" || strings.TrimSpace(public.Message) == "" {
		return nil, errors.New("rpc error mapper returned an incomplete public error"), nil
	}
	raw, mappingErr = json.Marshal(cmdrpc.ResponseEnvelope[any]{Error: public})
	return raw, mappingErr, nil
}
