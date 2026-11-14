package router

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/valyala/fasthttp"
)

// Setup is single-owner, like native registration. The mutex makes the closing
// boundary atomic; no application callback runs while it is held.
type fiberIngress struct {
	mu                 sync.Mutex
	closed, configured bool
	pending            error
	options            BoundedIngressOptions
	runtime            *ingressRuntime
	initialMethods     []string
}

func (i *fiberIngress) configure(o BoundedIngressOptions, validate func(BoundedIngressOptions) error) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return ingressConfigurationError("state", "closed")
	}
	if i.configured {
		return ingressConfigurationError("state", "already_configured")
	}
	normalized, err := o.Validated()
	if err == nil {
		err = validate(normalized)
	}
	i.pending = err
	if err != nil {
		return err
	}
	i.options, i.configured = normalized, true
	return nil
}
func (i *fiberIngress) close() { i.mu.Lock(); i.closed = true; i.mu.Unlock() }
func (i *fiberIngress) setup() (BoundedIngressOptions, bool, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.options, i.configured, i.pending
}

type ingressRuntime struct {
	options  BoundedIngressOptions
	methods  map[string]struct{}
	native   fasthttp.RequestHandler
	bridge   func(*fasthttp.RequestCtx, context.Context, fasthttp.RequestHandler)
	validate func() error
	// Native Test/Listener have no managed coordinator. Connection transitions
	// own their stream cleanup; no per-request goroutines or native Ctx retention.
	streamsMu sync.Mutex
	streams   map[net.Conn]context.CancelFunc
}

func (i *fiberIngress) install(server *fasthttp.Server, native fasthttp.RequestHandler, methods []string, guard *nativeRegistrationGuard, bridge func(*fasthttp.RequestCtx, context.Context, fasthttp.RequestHandler)) error {
	if server.GetOnly {
		return ingressConfigurationError("native.get_only", "unsupported")
	}
	if !slices.Equal(methods, i.initialMethods) {
		return ingressConfigurationError("native.methods", "mutated")
	}
	snapshot, err := snapshotIngressMethods(methods)
	if err != nil {
		return err
	}
	// A caller-installed handler (including TimeoutHandler) escapes synchronous
	// ownership. Never silently replace it and claim it is supported.
	if reflect.ValueOf(server.Handler).Pointer() != reflect.ValueOf(native).Pointer() {
		return ingressConfigurationError("native.handler", "unsupported")
	}
	if err := guard.validate(); err != nil {
		return err
	}
	expected := guard.capture()
	frozen := slices.Clone(methods)
	runtime := &ingressRuntime{options: i.options, methods: snapshot, native: native, bridge: bridge, streams: make(map[net.Conn]context.CancelFunc)}
	runtime.validate = func() error {
		if server.GetOnly {
			return ingressConfigurationError("native.get_only", "unsupported")
		}
		if !slices.Equal(methods, frozen) {
			return ingressConfigurationError("native.methods", "mutated")
		}
		if !reflect.DeepEqual(guard.capture(), expected) {
			return ErrManagedNativeRegistration
		}
		return nil
	}
	i.runtime = runtime
	server.Handler = runtime.standalone
	prior := server.ConnState
	server.ConnState = func(conn net.Conn, state fasthttp.ConnState) {
		if state == fasthttp.StateIdle || state == fasthttp.StateClosed || state == fasthttp.StateHijacked {
			runtime.streamsMu.Lock()
			cancel := runtime.streams[conn]
			delete(runtime.streams, conn)
			runtime.streamsMu.Unlock()
			if cancel != nil {
				cancel()
			}
		}
		if prior != nil {
			prior(conn, state)
		}
	}
	return nil
}
func (r *ingressRuntime) standalone(c *fasthttp.RequestCtx) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		if c.Response.IsBodyStream() {
			r.streamsMu.Lock()
			r.streams[c.Conn()] = cancel
			r.streamsMu.Unlock()
		} else {
			cancel()
		}
	}()
	r.handle(c, ctx, true)
}

func (r *ingressRuntime) diagnostic(ctx context.Context, d IngressDiagnostic) {
	if r.options.Diagnostic == nil {
		return
	}
	defer func() { _ = recover() }()
	r.options.Diagnostic(IngressCancellationFromContext(ctx), d)
}
func (r *ingressRuntime) observe(ctx context.Context, event IngressEvent) {
	if r.options.Observer == nil {
		return
	}
	defer func() {
		if p := recover(); p != nil {
			r.diagnostic(ctx, IngressDiagnostic{Phase: IngressObserverDiagnostic, RequestID: event.RequestID, Panic: p})
		}
	}()
	if err := r.options.Observer(IngressCancellationFromContext(ctx), event); err != nil {
		r.diagnostic(ctx, IngressDiagnostic{Phase: IngressObserverDiagnostic, RequestID: event.RequestID, Cause: err})
	}
}
func (r *ingressRuntime) decision(ctx context.Context, facts UnknownMethodRequest) (decision UnknownMethodDecision, err error, panicked any) {
	defer func() { panicked = recover() }()
	decision, err = r.options.UnknownMethod.Handler.HandleUnknownMethod(ctx, facts)
	return
}

type ingressResponseKey struct{}
type ingressResponseState struct{ written bool }

func markIngressWritten(c *fasthttp.RequestCtx) {
	restoreIngressResponseHeader(c)
	if state, ok := c.UserValue(ingressResponseKey{}).(*ingressResponseState); ok {
		state.written = true
	}
}
func ingressCommitted(c *fasthttp.RequestCtx) bool {
	if state, ok := c.UserValue(ingressResponseKey{}).(*ingressResponseState); ok && state.written {
		return true
	}
	return c.Response.IsBodyStream() || c.Hijacked() || c.LastTimeoutErrorResponse() != nil
}

var ingressFailure = UnknownMethodRejection{Status: 500, Code: "INGRESS_POLICY_FAILED", Message: "request policy failed"}
var ingressConnect = UnknownMethodRejection{Status: 501, Code: "HTTP_METHOD_NOT_IMPLEMENTED", Message: "method not implemented"}
var ingressInvalid = UnknownMethodRejection{Status: 400, Code: "HTTP_REQUEST_INVALID", Message: "invalid request"}
var ingressUnavailable = UnknownMethodRejection{Status: 503, Code: "HTTP_SERVICE_UNAVAILABLE", Message: "service unavailable"}

func (r *ingressRuntime) reject(c *fasthttp.RequestCtx, ctx context.Context, id string, rejection UnknownMethodRejection) {
	// The envelope has no arbitrary encoder or fields: serialization cannot invoke
	// application code. Validate again so every writer enforces the byte bound.
	body, err := json.Marshal(IngressErrorEnvelope{Code: rejection.Code, Message: rejection.Message, RequestID: id, Retryable: rejection.Retryable})
	if err != nil || len(body) > IngressMaxEnvelopeBytes {
		r.diagnostic(ctx, IngressDiagnostic{Phase: IngressSerializationDiagnostic, RequestID: id, Cause: ingressDecisionError("response", "serialization")})
		rejection = ingressFailure
		body, _ = json.Marshal(IngressErrorEnvelope{Code: rejection.Code, Message: rejection.Message, RequestID: id})
	}
	c.Response.Reset()
	c.SetStatusCode(rejection.Status)
	c.Response.Header.SetContentType("application/json")
	setIngressHeader(&c.Response.Header, id)
	c.SetBody(body)
}
func (r *ingressRuntime) handle(c *fasthttp.RequestCtx, parent context.Context, admitted bool) {
	id := uuid.NewString()
	c.SetUserValue(ingressResponseKey{}, &ingressResponseState{})
	ctx := newIngressContext(parent, id)
	c.SetUserValue(ingressContextKey{}, ctx.Value(ingressContextKey{}))
	setIngressHeader(&c.Request.Header, id)
	setIngressHeader(&c.Response.Header, id)
	event := IngressEvent{RequestID: id}
	defer func() {
		if p := recover(); p != nil {
			event.Outcome = IngressPolicyFailed
			r.diagnostic(ctx, IngressDiagnostic{Phase: IngressDispatchDiagnostic, RequestID: id, Panic: p})
			if !ingressCommitted(c) {
				r.reject(c, ctx, id, ingressFailure)
			}
		}
		// Restore the wire header; adapters also protect early upgrade handoffs.
		// Direct Conn writes and TimeoutError are unsupported native escape hatches.
		setIngressHeader(&c.Response.Header, id)
		event.Status, event.Committed = c.Response.StatusCode(), ingressCommitted(c)
		r.observe(ctx, event)
	}()
	method, target := string(c.Method()), string(c.Request.Header.RequestURI())
	form, valid := ingressTarget(method, target)
	if !validIngressMethodToken(method) || !valid {
		// Boundary validation is pre-admission: its callbacks receive an
		// already-canceled signal and must not start blocking work.
		canceled, cancel := context.WithCancel(parent)
		cancel()
		ctx = newIngressContext(canceled, id)
		c.SetUserValue(ingressContextKey{}, ctx.Value(ingressContextKey{}))
		event.Outcome = IngressInvalidRequest
		r.reject(c, ctx, id, ingressInvalid)
		return
	}

	if !admitted {
		event.Outcome = IngressAdmissionDenied
		r.reject(c, ctx, id, ingressUnavailable)
		c.SetConnectionClose()
		return
	}
	if err := r.validate(); err != nil {
		event.Outcome = IngressPolicyFailed
		r.diagnostic(ctx, IngressDiagnostic{Phase: IngressDispatchDiagnostic, RequestID: id, Cause: err})
		r.reject(c, ctx, id, ingressFailure)
		return
	}
	if method == "CONNECT" {
		event.Outcome = IngressConnectRejected
		r.reject(c, ctx, id, ingressConnect)
		return
	}
	if _, known := r.methods[method]; !known {
		policy := r.options.UnknownMethod
		switch policy.Policy {
		case UnknownMethodReject:
			event.Outcome = IngressRejected
			r.reject(c, ctx, id, policy.Rejection)
			return
		case UnknownMethodCustom:
			decision, err, p := r.decision(ctx, UnknownMethodRequest{Method: strings.Clone(method), TargetForm: form, RequestID: id})
			phase := IngressStrategyDiagnostic
			if err == nil && p == nil {
				decision, err = decision.Validated()
				phase = IngressDecisionDiagnostic
			}
			if err != nil || p != nil {
				event.Outcome = IngressPolicyFailed
				r.diagnostic(ctx, IngressDiagnostic{Phase: phase, RequestID: id, Cause: err, Panic: p})
				r.reject(c, ctx, id, ingressFailure)
				return
			}
			if decision.Action == RejectUnknownMethod {
				event.Outcome = IngressRejected
				r.reject(c, ctx, id, decision.Rejection)
				return
			}
		}
	}
	event.Outcome, event.Dispatches = IngressDelegated, 1
	r.bridge(c, ctx, r.native)
}

func ingressTarget(method, target string) (IngressTargetForm, bool) {
	if method == "CONNECT" {
		host, port, err := net.SplitHostPort(target)
		if err != nil || host == "" || port == "" || strings.ContainsAny(host, "/?#@% \t\r\n") {
			return IngressAuthorityForm, false
		}
		for _, b := range port {
			if b < '0' || b > '9' {
				return IngressAuthorityForm, false
			}
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return IngressAuthorityForm, false
		}
		if strings.HasPrefix(target, "[") && (net.ParseIP(host) == nil || !strings.Contains(host, ":")) {
			return IngressAuthorityForm, false
		}
		if net.ParseIP(host) == nil && !ingressHostname(host) {
			return IngressAuthorityForm, false
		}
		return IngressAuthorityForm, true
	}
	if target == "*" {
		return IngressAsteriskForm, method == "OPTIONS"
	}
	if strings.ContainsAny(target, "#\r\n\t ") {
		return 0, false
	}
	u, err := url.ParseRequestURI(target)
	if err != nil {
		return 0, false
	}
	if strings.HasPrefix(target, "/") {
		return IngressOriginForm, true
	}
	return IngressAbsoluteForm, (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}

func ingressHostname(host string) bool {
	host = strings.TrimSuffix(host, ".")
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
