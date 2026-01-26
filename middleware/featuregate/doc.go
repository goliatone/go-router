// Package featuregate provides go-router middleware for wiring go-featuregate
// scope and actor data into request contexts. This package is the only place in
// go-router that should import go-featuregate so the core router stays
// dependency-free.
//
// Usage:
//
//	mw := featuregate.New(
//		featuregate.WithClaimsResolver(func(ctx router.Context) (gate.ActorClaims, error) {
//			return gate.ActorClaims{
//				TenantID:  ctx.Param("tenant_id"),
//				OrgID:     ctx.Param("org_id"),
//				SubjectID: ctx.Locals("user_id").(string),
//			}, nil
//		}),
//	)
//
//	app.Use(mw)
//
//	app.Get("/users", func(ctx router.Context) error {
//		enabled, err := gate.Enabled(ctx.Context(), "users.read")
//		if err != nil {
//			return err
//		}
//		if !enabled {
//			return router.NewNotFoundError("not found")
//		}
//		return ctx.JSON(200, map[string]string{"ok": "true"})
//	})
package featuregate
