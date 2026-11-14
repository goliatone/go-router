package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	gerrors "github.com/goliatone/go-errors"
)

type nilIngressHandler struct{}

func (*nilIngressHandler) HandleUnknownMethod(context.Context, UnknownMethodRequest) (UnknownMethodDecision, error) {
	panic("must not be invoked")
}

type nilMapIngressHandler map[string]string

func (nilMapIngressHandler) HandleUnknownMethod(context.Context, UnknownMethodRequest) (UnknownMethodDecision, error) {
	panic("must not be invoked")
}

func TestBoundedIngressConfiguration(t *testing.T) {
	handler := UnknownMethodHandlerFunc(func(context.Context, UnknownMethodRequest) (UnknownMethodDecision, error) {
		t.Fatal("validation invoked callback")
		return UnknownMethodDecision{}, nil
	})
	valid := UnknownMethodRejection{Status: 429, Code: "LIMITED", Message: "try later"}
	cases := []struct {
		name    string
		options UnknownMethodOptions
		field   string
	}{
		{"unknown policy", UnknownMethodOptions{Policy: 255}, "policy"},
		{"missing custom", UnknownMethodOptions{Policy: UnknownMethodCustom}, "handler"},
		{"nil function", UnknownMethodOptions{Policy: UnknownMethodCustom, Handler: UnknownMethodHandlerFunc(nil)}, "handler"},
		{"typed nil pointer", UnknownMethodOptions{Policy: UnknownMethodCustom, Handler: (*nilIngressHandler)(nil)}, "handler"},
		{"typed nil map", UnknownMethodOptions{Policy: UnknownMethodCustom, Handler: nilMapIngressHandler(nil)}, "handler"},
		{"native handler", UnknownMethodOptions{Handler: handler}, "handler"},
		{"native typed nil", UnknownMethodOptions{Handler: (*nilIngressHandler)(nil)}, "handler"},
		{"reject handler", UnknownMethodOptions{Policy: UnknownMethodReject, Handler: handler}, "handler"},
		{"native rejection", UnknownMethodOptions{Rejection: valid}, "rejection"},
		{"custom rejection", UnknownMethodOptions{Policy: UnknownMethodCustom, Handler: handler, Rejection: valid}, "rejection"},
		{"partial rejection", UnknownMethodOptions{Policy: UnknownMethodReject, Rejection: UnknownMethodRejection{Status: 501}}, "rejection.code"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (BoundedIngressOptions{UnknownMethod: tc.options}).Validated()
			var typed *IngressConfigurationError
			var structured *gerrors.Error
			if !errors.Is(err, ErrIngressConfiguration) || !errors.As(err, &typed) || typed.Field != tc.field {
				t.Fatalf("wrong config error: %v", err)
			}
			if !errors.As(err, &structured) || structured.Code != 400 || structured.TextCode != "INGRESS_CONFIGURATION_INVALID" || structured.Category != gerrors.CategoryBadInput {
				t.Fatalf("lost structured validation: %+v", structured)
			}
		})
	}
	for _, o := range []UnknownMethodOptions{{}, {Policy: UnknownMethodReject}, {Policy: UnknownMethodReject, Rejection: valid}, {Policy: UnknownMethodCustom, Handler: handler}} {
		out, err := (BoundedIngressOptions{UnknownMethod: o}).Validated()
		if err != nil {
			t.Fatal(err)
		}
		if o.Policy == UnknownMethodReject && o.Rejection == (UnknownMethodRejection{}) && (out.UnknownMethod.Rejection.Status != 501 || out.UnknownMethod.Rejection.Code != "HTTP_METHOD_NOT_IMPLEMENTED" || out.UnknownMethod.Rejection.Message != "method not implemented") {
			t.Fatalf("incorrect defaults: %+v", out)
		}
	}
}

func TestBoundedIngressStatusContract(t *testing.T) {
	for status := -1; status <= 600; status++ {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			rejection := UnknownMethodRejection{Status: status, Code: "PUBLIC", Message: "public"}
			supported := status >= 400 && status <= 599 && status != 401 && status != 405 && status != 407 && status != 426
			_, configErr := (UnknownMethodOptions{Policy: UnknownMethodReject, Rejection: rejection}).Validated()
			_, decisionErr := (UnknownMethodDecision{Action: RejectUnknownMethod, Rejection: rejection}).Validated()
			if (configErr == nil) != supported || (decisionErr == nil) != supported {
				t.Fatalf("status=%d supported=%v config=%v decision=%v", status, supported, configErr, decisionErr)
			}
			if !supported && (!errors.Is(configErr, ErrIngressConfiguration) || !errors.Is(decisionErr, ErrIngressDecision)) {
				t.Fatal("lost error identity")
			}
		})
	}
}

func TestBoundedIngressRejectionBounds(t *testing.T) {
	cases := []struct {
		name, code, message string
		valid               bool
	}{
		{"minimum", "A", "m", true}, {"max code", strings.Repeat("A", 64), "m", true},
		{"max unicode", "PUBLIC", strings.Repeat("😀", 200), true},
		{"escaped controls", "PUBLIC", strings.Repeat("\x00", 200), true},
		{"html escaping", "PUBLIC", strings.Repeat("<", 200), true},
		{"empty code", "", "m", false}, {"long code", strings.Repeat("A", 65), "m", false},
		{"lowercase", "secret-code", "m", false}, {"nonascii", "É", "m", false},
		{"empty message", "PUBLIC", "", false}, {"long message", "PUBLIC", strings.Repeat("m", 201), false},
		{"long bytes", "PUBLIC", strings.Repeat("😀", 201), false}, {"invalid utf8", "PUBLIC", string([]byte{255}), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := UnknownMethodRejection{Status: 503, Code: tc.code, Message: tc.message}
			_, err := (UnknownMethodOptions{Policy: UnknownMethodReject, Rejection: r}).Validated()
			_, derr := (UnknownMethodDecision{Action: RejectUnknownMethod, Rejection: r}).Validated()
			if (err == nil) != tc.valid || (derr == nil) != tc.valid {
				t.Fatalf("valid=%v config=%v decision=%v", tc.valid, err, derr)
			}
			if tc.valid {
				bytes, err := json.Marshal(IngressErrorEnvelope{tc.code, tc.message, strings.Repeat("x", IngressMaxRequestIDBytes), nil})
				if err != nil || len(bytes) > IngressMaxEnvelopeBytes {
					t.Fatalf("envelope size %d: %v", len(bytes), err)
				}
			} else if strings.Contains(fmt.Sprint(err), "secret-code") {
				t.Fatal("validation leaked input")
			}
		})
	}
}

func TestBoundedIngressDecisionAndOwnership(t *testing.T) {
	for _, d := range []UnknownMethodDecision{{}, {Action: 255}, {Action: RejectUnknownMethod}, {Action: DelegateNative, Rejection: UnknownMethodRejection{Status: 501}}} {
		_, err := d.Validated()
		var typed *IngressDecisionError
		var structured *gerrors.Error
		if !errors.Is(err, ErrIngressDecision) || !errors.As(err, &typed) || !errors.As(err, &structured) || structured.Code != 500 || structured.TextCode != "INGRESS_POLICY_FAILED" {
			t.Fatalf("wrong decision error: %v", err)
		}
	}
	if _, err := (UnknownMethodDecision{Action: DelegateNative}).Validated(); err != nil {
		t.Fatal(err)
	}
	retry := true
	r := UnknownMethodRejection{Status: 503, Code: "RETRY", Message: "try later", Retryable: &retry}
	options, err := (UnknownMethodOptions{Policy: UnknownMethodReject, Rejection: r}).Validated()
	if err != nil {
		t.Fatal(err)
	}
	decision, err := (UnknownMethodDecision{Action: RejectUnknownMethod, Rejection: r}).Validated()
	if err != nil {
		t.Fatal(err)
	}
	retry = false
	if !*options.Rejection.Retryable || !*decision.Rejection.Retryable {
		t.Fatal("retained caller pointer")
	}
	*options.Rejection.Retryable = false
	if !*decision.Rejection.Retryable {
		t.Fatal("snapshots share mutable state")
	}
	snapshot, err := snapshotIngressMethods([]string{"GET", "get", "X-CUSTOM", "!#$%&'*+-.^_`|~"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot) != 4 {
		t.Fatal("normalized method tokens")
	}
	methods := []string{"GET"}
	snapshot, err = snapshotIngressMethods(methods)
	if err != nil {
		t.Fatal(err)
	}
	methods[0] = "POST"
	if _, ok := snapshot["GET"]; !ok {
		t.Fatal("method metadata aliased")
	}
	for _, methods := range [][]string{nil, {}, {"GET", "GET"}, {""}, {"GET "}, {"GÉT"}, {"GET\r\n"}, {"G/T"}} {
		if _, err := snapshotIngressMethods(methods); !errors.Is(err, ErrIngressConfiguration) {
			t.Fatalf("invalid metadata accepted: %q", methods)
		}
	}
}

func TestBoundedIngressFunctionAdapter(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "trusted")
	request := UnknownMethodRequest{Method: "X-CUSTOM", TargetForm: IngressOriginForm, RequestID: "opaque"}
	calls := 0
	cause := errors.New("privileged")
	handler := UnknownMethodHandlerFunc(func(got context.Context, r UnknownMethodRequest) (UnknownMethodDecision, error) {
		calls++
		if got != ctx || r != request {
			t.Fatal("changed input")
		}
		return UnknownMethodDecision{Action: DelegateNative}, cause
	})
	decision, err := handler.HandleUnknownMethod(ctx, request)
	if calls != 1 || decision.Action != DelegateNative || err != cause {
		t.Fatal("function adapter changed result")
	}
	if _, err := UnknownMethodHandlerFunc(nil).HandleUnknownMethod(ctx, request); !errors.Is(err, ErrIngressDecision) {
		t.Fatal("nil function not inspectable")
	}
	if _, ok := IngressRequestIDFromContext(ctx); ok {
		t.Fatal("legacy context acquired ingress ID")
	}
	idctx := context.WithValue(ctx, ingressRequestIDKey{}, "opaque")
	if id, ok := IngressRequestIDFromContext(idctx); !ok || id != "opaque" {
		t.Fatal("ID unavailable")
	}
	if _, ok := IngressRequestIDFromContext(nil); ok {
		t.Fatal("nil context acquired ingress ID")
	}
}

// A custom context can return a diagnostic error from Err. Even that must not
// turn the cancellation-only view into an error/secret propagation channel.
type ingressDiagnosticContext struct{ context.Context }

func (ingressDiagnosticContext) Err() error    { return errors.New("private diagnostic") }
func (ingressDiagnosticContext) Value(any) any { panic("must not access values") }
func TestBoundedIngressCancellationSanitizesCustomError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	view := IngressCancellationFromContext(ingressDiagnosticContext{ctx})
	if view.Err() != context.Canceled {
		t.Fatal("noncanonical error exposed")
	}
	select {
	case <-view.Done():
	default:
		t.Fatal("source cancellation missing")
	}
}
