package router

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	gerrors "github.com/goliatone/go-errors"
)

// BoundedIngressServer is the optional construction-time configuration contract.
// Success validates and copies options; it does NOT prepare routes, install the
// wrapper, bind a listener or signal readiness. Routes remain configurable until
// initialization begins. One successful configuration is allowed per adapter;
// subsequent calls fail without replacing the accepted policy.
//
// An invalid first configuration leaves a pending setup error, not legacy
// fallback. A valid retry before initialization clears that error. Beginning
// Initialize/Init, WrappedRouter, Serve or StartManaged closes configuration,
// even if startup fails. Calls after that boundary fail without changing state.
// Initialize reports a pending setup error or performs final native validation,
// freezes methods and installs the policy. Its success establishes installation,
// not serving readiness; its failures are sticky. Raw native Test/Listener use
// requires successful adapter initialization first (WrappedRouter prepares it).
//
// Effective native GETOnly (Config or Server) is incompatible and rejected
// during preflight/activation; managed startup rechecks before accepting.
// DisableHeaderNormalizing remains supported: only X-Request-ID is deduplicated.
// Context replacement preserves application cancellation/deadlines and values,
// bounded by the ingress-owned lifetime; safe callbacks retain that owned lifetime.
// Both native Fiber adapters implement this optional capability.
// Required Server/Router/Context interfaces remain unchanged.
type BoundedIngressServer interface {
	ConfigureBoundedIngress(BoundedIngressOptions) error
}

// BoundedIngressValidator checks configuration and current native method
// metadata without preparing routes, invoking callbacks or enabling ingress.
// Success is not runtime readiness: activation must revalidate and freeze the
// final method set. Call only during single-owner construction, before serving.
type BoundedIngressValidator interface {
	ValidateBoundedIngress(BoundedIngressOptions) error
}

// BoundedIngressOptions opts into fresh server correlation, fixed CONNECT
// safety and global unknown-method policy when activated. The zero value selects
// Native. Absence of activation preserves legacy behavior. Observer/Diagnostic
// are synchronous, receive cancellation-only input, must cooperate with it, and
// must be concurrency-safe. A blocked noncooperative callback may delay final
// completion; the runner does not abandon callback goroutines or force termination.
// No caller-supplied serializer, ID generator, resolver or response writer is used.
type BoundedIngressOptions struct {
	UnknownMethod UnknownMethodOptions
	Observer      IngressObserver
	Diagnostic    IngressDiagnosticHandler
}

// Validated returns a normalized independent copy, including Retryable. It does
// not enable ingress or verify native capabilities. Callback state remains owned
// by the application. Do not mutate input concurrently with validation.
func (o BoundedIngressOptions) Validated() (BoundedIngressOptions, error) {
	u, err := o.UnknownMethod.Validated()
	if err != nil {
		return BoundedIngressOptions{}, err
	}
	o.UnknownMethod = u
	return o, nil
}

type UnknownMethodPolicy uint8

const (
	UnknownMethodNative UnknownMethodPolicy = iota
	UnknownMethodReject
	UnknownMethodCustom
)

type UnknownMethodOptions struct {
	Policy    UnknownMethodPolicy
	Rejection UnknownMethodRejection // Only Reject; entirely zero selects defaults.
	Handler   UnknownMethodHandler   // Only Custom; nil and typed-nil are invalid.
}

// UnknownMethodHandler decides policy, not routing facts. It receives owned
// strings and a standard request context (no native context, writer or next).
// The runner invokes it once, synchronously outside locks, only for globally
// unknown ordinary-target methods after admission/CONNECT checks. Implementations
// must not dispatch/write through captured references or retain request resources.
// Returned rejection data transfers ownership; do not mutate it after return.
// Errors/panics/invalid decisions cause fixed safe 500, never native delegation.
type UnknownMethodHandler interface {
	HandleUnknownMethod(context.Context, UnknownMethodRequest) (UnknownMethodDecision, error)
}
type UnknownMethodHandlerFunc func(context.Context, UnknownMethodRequest) (UnknownMethodDecision, error)

func (f UnknownMethodHandlerFunc) HandleUnknownMethod(ctx context.Context, r UnknownMethodRequest) (UnknownMethodDecision, error) {
	if f == nil {
		return UnknownMethodDecision{}, ingressDecisionError("handler", "nil")
	}
	return f(ctx, r)
}

type UnknownMethodRequest struct {
	Method     string
	TargetForm IngressTargetForm
	RequestID  string
}
type IngressTargetForm uint8

const (
	IngressOriginForm IngressTargetForm = iota + 1
	IngressAbsoluteForm
	IngressAuthorityForm
	IngressAsteriskForm
)

type UnknownMethodAction uint8

const (
	DelegateNative UnknownMethodAction = iota + 1
	RejectUnknownMethod
)

type UnknownMethodDecision struct {
	Action    UnknownMethodAction
	Rejection UnknownMethodRejection // Must be entirely zero for DelegateNative.
}

// UnknownMethodRejection is deliberately header-free. Status must be 400–599
// excluding 401, 405, 407 and 426. Code is 1–64 ASCII A-Z/0-9/underscore bytes;
// Message is 1–200 Unicode characters, valid UTF-8, at most 800 bytes. Public
// text is application-owned, not automatically redacted. Retryable is optional.
type UnknownMethodRejection struct {
	Status    int
	Code      string
	Message   string
	Retryable *bool
}

const (
	IngressMaxCodeBytes         = 64
	IngressMaxMessageCharacters = 200
	IngressMaxMessageBytes      = 800
	IngressMaxRequestIDBytes    = 128
	IngressMaxEnvelopeBytes     = 2048
)

// IngressErrorEnvelope is the only ingress-owned JSON output. Native/application
// responses keep their serializers. RequestID is a fresh opaque server ID using ASCII A-Z/a-z/0-9/_/-;
// public code/message never originate from an error or panic string.
type IngressErrorEnvelope struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Retryable *bool  `json:"retryable,omitempty"`
}

// IngressOutcome is a bounded observation label, never a raw method/path label.
type IngressOutcome uint8

const (
	IngressDelegated IngressOutcome = iota + 1
	IngressRejected
	IngressPolicyFailed
	IngressAdmissionDenied
	IngressInvalidRequest
	IngressConnectRejected
)

// IngressEvent contains no raw method, target, identity, credentials or cause.
// Status is the response status at observation, not a final stream outcome.
// Dispatches is zero or one. Committed also covers handed-off streams/hijacks.
type IngressEvent struct {
	Outcome    IngressOutcome
	Status     int
	RequestID  string
	Dispatches uint8
	Committed  bool
}

// IngressObserver is attempted once per boundary request, without recursion on
// error/panic. Cancellation follows the admitted request lifetime through the
// callback. No context values, raw cancellation cause or native objects are exposed.
type IngressObserver func(IngressCancellation, IngressEvent) error

// IngressCancellation is an opaque, read-only view of a callback's lifetime.
// It exposes no Value, Cause, cancellation method or underlying context, and is
// deliberately not a context.Context. Copies share the same cancellation signal;
// methods are safe for concurrent calls. The zero value never cancels and has no
// deadline. Do not retain it beyond the callback/request lifetime.
type IngressCancellation struct {
	done        <-chan struct{}
	deadline    time.Time
	hasDeadline bool
	err         func() error
}

// IngressCancellationFromContext projects a standard request context without
// exposing values or a raw cancellation cause. Nil produces the zero view.
// It creates no goroutine, timer or new lifetime. The source must obey the
// context.Context concurrency/deadline contract; do not pass a pooled native ctx.
func IngressCancellationFromContext(ctx context.Context) IngressCancellation {
	if ctx == nil {
		return IngressCancellation{}
	}
	deadline, ok := ctx.Deadline()
	return IngressCancellation{
		done: ctx.Done(), deadline: deadline, hasDeadline: ok,
		err: func() error {
			switch ctx.Err() {
			case nil:
				return nil
			case context.DeadlineExceeded:
				return context.DeadlineExceeded
			default:
				return context.Canceled
			}
		},
	}
}

// Done closes when the source lifetime is canceled. A nil channel means that
// lifetime cannot be canceled. It does not signal callback completion.
func (c IngressCancellation) Done() <-chan struct{} { return c.done }

// Deadline returns the source deadline, if any. Managed drain can cancel a
// request after an interval without giving its context a fixed deadline; in
// that case Done is authoritative and Deadline's boolean is false.
func (c IngressCancellation) Deadline() (time.Time, bool) { return c.deadline, c.hasDeadline }

// Err returns only nil, context.Canceled or context.DeadlineExceeded. It never
// returns context.Cause or a custom context's diagnostic error.
func (c IngressCancellation) Err() error {
	if c.err == nil {
		return nil
	}
	return c.err()
}

type IngressDiagnosticPhase uint8

const (
	IngressStrategyDiagnostic IngressDiagnosticPhase = iota + 1
	IngressDecisionDiagnostic
	IngressSerializationDiagnostic
	IngressObserverDiagnostic
	IngressDispatchDiagnostic
)

// IngressDiagnostic is privileged. Cause and Panic may contain secrets; logging
// and retention are application-owned. A diagnostic failure must be swallowed,
// never reported recursively or substituted into a public response/event.
type IngressDiagnostic struct {
	Phase     IngressDiagnosticPhase
	RequestID string
	Cause     error
	Panic     any
}

// IngressDiagnosticHandler receives the same cancellation view as the observer,
// including when cancellation has already occurred. Failure must not recurse.
type IngressDiagnosticHandler func(IngressCancellation, IngressDiagnostic)

var (
	ErrIngressConfiguration = errors.New("invalid bounded ingress configuration")
	ErrIngressDecision      = errors.New("invalid unknown-method decision")
)

// IngressConfigurationError contains only fixed field/rule labels, never input
// values. errors.Is matches ErrIngressConfiguration; errors.As also exposes the
// wrapped structured go-errors error for existing diagnostic tooling.
type IngressConfigurationError struct {
	Field, Rule string
	err         error
}

func (e *IngressConfigurationError) Error() string {
	return "invalid bounded ingress configuration: " + e.Field + " (" + e.Rule + ")"
}
func (e *IngressConfigurationError) Unwrap() error { return e.err }

// IngressDecisionError is a diagnostic classification, not a public response.
// The runner maps it to fixed 500/INGRESS_POLICY_FAILED/request policy failed.
type IngressDecisionError struct {
	Field, Rule string
	err         error
}

func (e *IngressDecisionError) Error() string {
	return "invalid unknown-method decision: " + e.Field + " (" + e.Rule + ")"
}
func (e *IngressDecisionError) Unwrap() error { return e.err }
func ingressConfigurationError(field, rule string) error {
	return &IngressConfigurationError{field, rule, gerrors.Wrap(ErrIngressConfiguration, gerrors.CategoryBadInput, "invalid bounded ingress configuration").WithCode(400).WithTextCode("INGRESS_CONFIGURATION_INVALID").WithMetadata(map[string]any{"field": field, "rule": rule})}
}
func ingressDecisionError(field, rule string) error {
	return &IngressDecisionError{field, rule, gerrors.Wrap(ErrIngressDecision, gerrors.CategoryInternal, "request policy failed").WithCode(500).WithTextCode("INGRESS_POLICY_FAILED").WithMetadata(map[string]any{"field": field, "rule": rule})}
}

func (o UnknownMethodOptions) Validated() (UnknownMethodOptions, error) {
	switch o.Policy {
	case UnknownMethodNative, UnknownMethodReject:
		if o.Handler != nil {
			return UnknownMethodOptions{}, ingressConfigurationError("handler", "conflicting")
		}
	case UnknownMethodCustom:
		if nilUnknownMethodHandler(o.Handler) {
			return UnknownMethodOptions{}, ingressConfigurationError("handler", "required")
		}
	default:
		return UnknownMethodOptions{}, ingressConfigurationError("policy", "unsupported")
	}
	if o.Policy != UnknownMethodReject {
		if o.Rejection != (UnknownMethodRejection{}) {
			return UnknownMethodOptions{}, ingressConfigurationError("rejection", "conflicting")
		}
	} else {
		if o.Rejection == (UnknownMethodRejection{}) {
			o.Rejection = UnknownMethodRejection{Status: 501, Code: "HTTP_METHOD_NOT_IMPLEMENTED", Message: "method not implemented"}
		}
		if field, rule := rejectionViolation(o.Rejection); field != "" {
			return UnknownMethodOptions{}, ingressConfigurationError(field, rule)
		}
		o.Rejection = cloneIngressRejection(o.Rejection)
	}
	return o, nil
}

// Validated checks an application decision without applying built-in defaults.
// The returned Retryable belongs to the caller independently of the input.
func (d UnknownMethodDecision) Validated() (UnknownMethodDecision, error) {
	switch d.Action {
	case DelegateNative:
		if d.Rejection != (UnknownMethodRejection{}) {
			return UnknownMethodDecision{}, ingressDecisionError("rejection", "conflicting")
		}
	case RejectUnknownMethod:
		if field, rule := rejectionViolation(d.Rejection); field != "" {
			return UnknownMethodDecision{}, ingressDecisionError(field, rule)
		}
	default:
		return UnknownMethodDecision{}, ingressDecisionError("action", "unsupported")
	}
	d.Rejection = cloneIngressRejection(d.Rejection)
	return d, nil
}
func nilUnknownMethodHandler(h UnknownMethodHandler) bool {
	if h == nil {
		return true
	}
	v := reflect.ValueOf(h)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}
func cloneIngressRejection(r UnknownMethodRejection) UnknownMethodRejection {
	if r.Retryable != nil {
		value := *r.Retryable
		r.Retryable = &value
	}
	return r
}
func rejectionViolation(r UnknownMethodRejection) (string, string) {
	if r.Status < 400 || r.Status > 599 || r.Status == 401 || r.Status == 405 || r.Status == 407 || r.Status == 426 {
		return "rejection.status", "unsupported"
	}
	if len(r.Code) == 0 || len(r.Code) > IngressMaxCodeBytes {
		return "rejection.code", "bounds"
	}
	for _, b := range []byte(r.Code) {
		if !(b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_') {
			return "rejection.code", "syntax"
		}
	}
	if r.Message == "" || !utf8.ValidString(r.Message) || len(r.Message) > IngressMaxMessageBytes || utf8.RuneCountInString(r.Message) > IngressMaxMessageCharacters {
		return "rejection.message", "bounds"
	}
	// Worst-case size for a generated unescaped ID, including JSON escaping
	// of public fields. The
	// runtime must validate its actual ID and serialized bytes as well.
	body, err := json.Marshal(IngressErrorEnvelope{r.Code, r.Message, strings.Repeat("x", IngressMaxRequestIDBytes), r.Retryable})
	if err != nil || len(body) > IngressMaxEnvelopeBytes {
		return "rejection", "serialized_bounds"
	}
	return "", ""
}

// IngressRequestIDFromContext returns the server-owned ingress correlation ID.
// It is not an identity/authorization claim. Without activated ingress it is absent.
func IngressRequestIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	id, ok := ctx.Value(ingressRequestIDKey{}).(string)
	return id, ok && id != ""
}

type ingressRequestIDKey struct{}
