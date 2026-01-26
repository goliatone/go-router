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

func New(opts ...Option) router.MiddlewareFunc {
	cfg := newConfig(opts...)

	return func(_ router.HandlerFunc) router.HandlerFunc {
		return func(ctx router.Context) error {
			if cfg.claimsResolver == nil {
				return ctx.Next()
			}

			claims, err := cfg.claimsResolver(ctx)
			if err != nil {
				return err
			}

			updated := applyClaims(ctx.Context(), claims)
			ctx.SetContext(updated)

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
