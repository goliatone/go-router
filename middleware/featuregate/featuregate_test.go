package featuregate_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-featuregate/gate"
	"github.com/goliatone/go-featuregate/scope"
	"github.com/goliatone/go-router"
	"github.com/goliatone/go-router/middleware/featuregate"
	"github.com/stretchr/testify/mock"
)

func TestMiddleware_ClaimsResolver_SetsScope(t *testing.T) {
	ctx := router.NewMockContext()
	ctx.On("Context").Return(context.Background())
	ctx.On("SetContext", mock.MatchedBy(func(updated context.Context) bool {
		return scope.TenantID(updated) == "tenant-1" &&
			scope.OrgID(updated) == "org-1" &&
			scope.UserID(updated) == "user-1"
	})).Return()

	mw := featuregate.New(featuregate.WithClaimsResolver(func(ctx router.Context) (gate.ActorClaims, error) {
		return gate.ActorClaims{
			TenantID:  "tenant-1",
			OrgID:     "org-1",
			SubjectID: "user-1",
		}, nil
	}))

	handler := mw(func(ctx router.Context) error {
		return nil
	})

	if err := handler(ctx); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !ctx.NextCalled {
		t.Error("expected Next to be called")
	}

	ctx.AssertExpectations(t)
}

func TestMiddleware_NoResolver_NoContextUpdate(t *testing.T) {
	ctx := router.NewMockContext()
	mw := featuregate.New()
	handler := mw(func(ctx router.Context) error {
		return nil
	})

	if err := handler(ctx); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !ctx.NextCalled {
		t.Error("expected Next to be called")
	}

	ctx.AssertNotCalled(t, "Context")
	ctx.AssertNotCalled(t, "SetContext", mock.Anything)
}

func TestMiddleware_ClaimsResolver_Error_ReturnsError(t *testing.T) {
	ctx := router.NewMockContext()
	ctx.On("Context").Return(context.Background())

	expectedErr := errors.New("resolver failed")
	mw := featuregate.New(featuregate.WithClaimsResolver(func(ctx router.Context) (gate.ActorClaims, error) {
		return gate.ActorClaims{}, expectedErr
	}))

	handler := mw(func(ctx router.Context) error {
		return nil
	})

	err := handler(ctx)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected resolver error, got %v", err)
	}

	if ctx.NextCalled {
		t.Error("expected Next not to be called")
	}

	ctx.AssertNotCalled(t, "SetContext", mock.Anything)
	ctx.AssertExpectations(t)
}

func TestMiddleware_ActorResolver_SetsActorRef(t *testing.T) {
	ctx := router.NewMockContext()
	ctx.On("Context").Return(context.Background())
	ctx.On("SetContext", mock.MatchedBy(func(updated context.Context) bool {
		actor, ok := featuregate.ActorFromContext(updated)
		if !ok {
			return false
		}
		return actor.ID == "user-1" && actor.Type == "user" && actor.Name == "User One"
	})).Return()

	mw := featuregate.New(featuregate.WithActorResolver(func(ctx router.Context) gate.ActorRef {
		return gate.ActorRef{
			ID:   "user-1",
			Type: "user",
			Name: "User One",
		}
	}))

	handler := mw(func(ctx router.Context) error {
		return nil
	})

	if err := handler(ctx); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !ctx.NextCalled {
		t.Error("expected Next to be called")
	}

	ctx.AssertExpectations(t)
}

func TestMiddleware_ActorResolver_EmptyActor_NoContextUpdate(t *testing.T) {
	ctx := router.NewMockContext()
	ctx.On("Context").Return(context.Background())

	mw := featuregate.New(featuregate.WithActorResolver(func(ctx router.Context) gate.ActorRef {
		return gate.ActorRef{}
	}))

	handler := mw(func(ctx router.Context) error {
		return nil
	})

	if err := handler(ctx); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !ctx.NextCalled {
		t.Error("expected Next to be called")
	}

	ctx.AssertNotCalled(t, "SetContext", mock.Anything)
	ctx.AssertExpectations(t)
}

func TestMiddleware_StrictMode_MissingClaims_ReturnsBadRequest(t *testing.T) {
	ctx := router.NewMockContext()
	ctx.On("Context").Return(context.Background())

	mw := featuregate.New(
		featuregate.WithStrict(true),
		featuregate.WithClaimsResolver(func(ctx router.Context) (gate.ActorClaims, error) {
			return gate.ActorClaims{}, nil
		}),
	)

	handler := mw(func(ctx router.Context) error {
		return nil
	})

	err := handler(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var rich *goerrors.Error
	if !errors.As(err, &rich) {
		t.Fatalf("expected router error, got %T", err)
	}
	if rich.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rich.Code)
	}

	if ctx.NextCalled {
		t.Error("expected Next not to be called")
	}

	ctx.AssertNotCalled(t, "SetContext", mock.Anything)
	ctx.AssertExpectations(t)
}

func TestMiddleware_StrictMode_ValidClaims_AllowsRequest(t *testing.T) {
	ctx := router.NewMockContext()
	ctx.On("Context").Return(context.Background())
	ctx.On("SetContext", mock.Anything).Return()

	mw := featuregate.New(
		featuregate.WithStrict(true),
		featuregate.WithClaimsResolver(func(ctx router.Context) (gate.ActorClaims, error) {
			return gate.ActorClaims{
				TenantID:  "tenant-1",
				SubjectID: "user-1",
			}, nil
		}),
	)

	handler := mw(func(ctx router.Context) error {
		return nil
	})

	if err := handler(ctx); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !ctx.NextCalled {
		t.Error("expected Next to be called")
	}

	ctx.AssertExpectations(t)
}

func TestMiddleware_StrictMode_ResolverError_ReturnsBadRequest(t *testing.T) {
	ctx := router.NewMockContext()
	ctx.On("Context").Return(context.Background())

	mw := featuregate.New(
		featuregate.WithStrict(true),
		featuregate.WithClaimsResolver(func(ctx router.Context) (gate.ActorClaims, error) {
			return gate.ActorClaims{}, errors.New("resolver failed")
		}),
	)

	handler := mw(func(ctx router.Context) error {
		return nil
	})

	err := handler(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var rich *goerrors.Error
	if !errors.As(err, &rich) {
		t.Fatalf("expected router error, got %T", err)
	}
	if rich.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rich.Code)
	}

	if ctx.NextCalled {
		t.Error("expected Next not to be called")
	}

	ctx.AssertNotCalled(t, "SetContext", mock.Anything)
	ctx.AssertExpectations(t)
}

func TestContextHelper_ReturnsUnderlyingContext(t *testing.T) {
	ctx := router.NewMockContext()
	expected := context.WithValue(context.Background(), "key", "value")
	ctx.On("Context").Return(expected)

	got := featuregate.Context(ctx)
	if got != expected {
		t.Fatal("expected helper to return the underlying context")
	}

	ctx.AssertExpectations(t)
}

func TestContextHelper_NilContext_ReturnsBackground(t *testing.T) {
	got := featuregate.Context(nil)
	if got == nil {
		t.Fatal("expected non-nil context")
	}
}
