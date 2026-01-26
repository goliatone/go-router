package featuregate_test

import (
	"context"
	"testing"

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
