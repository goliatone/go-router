package featuregate

import (
	"context"
	"strings"

	"github.com/goliatone/go-featuregate/gate"
	"github.com/goliatone/go-featuregate/scope"
	"github.com/goliatone/go-router"
)

type Option func(*config)

type ClaimsResolver func(router.Context) (gate.ActorClaims, error)

type ActorResolver func(router.Context) gate.ActorRef

type config struct {
	claimsResolver ClaimsResolver
	actorResolver  ActorResolver
	strict         bool
}

type actorContextKey struct{}

var actorKey actorContextKey

func New(opts ...Option) router.MiddlewareFunc {
	cfg := newConfig(opts...)

	return func(_ router.HandlerFunc) router.HandlerFunc {
		return func(ctx router.Context) error {
			if cfg.claimsResolver == nil && cfg.actorResolver == nil {
				return ctx.Next()
			}

			base := ctx.Context()
			updated := base

			if cfg.claimsResolver != nil {
				claims, err := cfg.claimsResolver(ctx)
				if err != nil {
					if cfg.strict {
						return router.NewBadRequestError("featuregate claims resolver failed",
							map[string]any{"error": err.Error()})
					}
					return err
				}
				if cfg.strict {
					if err := validateStrictClaims(claims); err != nil {
						return err
					}
				}
				updated = applyClaims(updated, claims)
			}

			if cfg.actorResolver != nil {
				actor := cfg.actorResolver(ctx)
				updated = withActorRef(updated, actor)
			}

			if updated != base {
				ctx.SetContext(updated)
			}

			return ctx.Next()
		}
	}
}

func WithClaimsResolver(resolver ClaimsResolver) Option {
	return func(cfg *config) {
		cfg.claimsResolver = resolver
	}
}

func WithActorResolver(resolver ActorResolver) Option {
	return func(cfg *config) {
		cfg.actorResolver = resolver
	}
}

func WithStrict(strict bool) Option {
	return func(cfg *config) {
		cfg.strict = strict
	}
}

func newConfig(opts ...Option) config {
	cfg := config{}

	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&cfg)
	}

	return cfg
}

// ActorFromContext returns the actor reference stored by the middleware.
func ActorFromContext(ctx context.Context) (gate.ActorRef, bool) {
	if ctx == nil {
		return gate.ActorRef{}, false
	}
	value := ctx.Value(actorKey)
	if value == nil {
		return gate.ActorRef{}, false
	}
	actor, ok := value.(gate.ActorRef)
	if !ok || actorRefEmpty(actor) {
		return gate.ActorRef{}, false
	}
	return actor, true
}

// Context returns the standard context from a router.Context.
func Context(ctx router.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx.Context()
}

func applyClaims(ctx context.Context, claims gate.ActorClaims) context.Context {
	if strings.TrimSpace(claims.TenantID) != "" {
		ctx = scope.WithTenantID(ctx, claims.TenantID)
	}
	if strings.TrimSpace(claims.OrgID) != "" {
		ctx = scope.WithOrgID(ctx, claims.OrgID)
	}
	if strings.TrimSpace(claims.SubjectID) != "" {
		ctx = scope.WithUserID(ctx, claims.SubjectID)
	}
	return ctx
}

func validateStrictClaims(claims gate.ActorClaims) error {
	hasTenant := strings.TrimSpace(claims.TenantID) != ""
	hasOrg := strings.TrimSpace(claims.OrgID) != ""
	hasUser := strings.TrimSpace(claims.SubjectID) != ""
	if hasTenant || hasOrg || hasUser {
		return nil
	}

	return router.NewBadRequestError("featuregate claims missing required identifiers",
		map[string]any{"missing": []string{
			scope.MetadataTenantID,
			scope.MetadataOrgID,
			scope.MetadataUserID,
		}})
}

func withActorRef(ctx context.Context, actor gate.ActorRef) context.Context {
	if actorRefEmpty(actor) {
		return ctx
	}
	return context.WithValue(ctx, actorKey, actor)
}

func actorRefEmpty(actor gate.ActorRef) bool {
	return strings.TrimSpace(actor.ID) == "" &&
		strings.TrimSpace(actor.Type) == "" &&
		strings.TrimSpace(actor.Name) == ""
}
