package router

import (
	"context"
	f3 "github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
	"strings"
)

var (
	_ BoundedIngressServer    = (*FiberAdapter)(nil)
	_ BoundedIngressServer    = (*FiberV3Adapter)(nil)
	_ BoundedIngressValidator = (*FiberAdapter)(nil)
	_ BoundedIngressValidator = (*FiberV3Adapter)(nil)
)

// ValidateBoundedIngress is construction-time preflight, not activation or
// finalization. It does not change the app, acquire a context or invoke policy.
func (a *FiberAdapter) ValidateBoundedIngress(o BoundedIngressOptions) error {
	if _, err := o.Validated(); err != nil {
		return err
	}
	if a == nil || a.app == nil {
		return ingressConfigurationError("native.methods", "unavailable")
	}
	if a.app.Config().GETOnly || a.app.Server().GetOnly {
		return ingressConfigurationError("native.get_only", "unsupported")
	}
	_, err := snapshotIngressMethods(a.app.Config().RequestMethods)
	return err
}

// ValidateBoundedIngress is the v3 equivalent of FiberAdapter preflight. It
// makes no claim about custom-context compatibility or published readiness.
func (a *FiberV3Adapter) ValidateBoundedIngress(o BoundedIngressOptions) error {
	if _, err := o.Validated(); err != nil {
		return err
	}
	if a == nil || a.app == nil {
		return ingressConfigurationError("native.methods", "unavailable")
	}
	if a.app.Config().GETOnly || a.app.Server().GetOnly {
		return ingressConfigurationError("native.get_only", "unsupported")
	}
	_, err := snapshotIngressMethods(a.app.Config().RequestMethods)
	return err
}

// Snapshot only configured native methods, never possible request methods or
// route evidence. Final activation must revalidate under exclusive setup ownership.
func snapshotIngressMethods(methods []string) (map[string]struct{}, error) {
	if len(methods) == 0 {
		return nil, ingressConfigurationError("native.methods", "unavailable")
	}
	snapshot := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		if !validIngressMethodToken(method) {
			return nil, ingressConfigurationError("native.methods", "syntax")
		}
		if _, exists := snapshot[method]; exists {
			return nil, ingressConfigurationError("native.methods", "duplicate")
		}
		snapshot[strings.Clone(method)] = struct{}{}
	}
	return snapshot, nil
}
func validIngressMethodToken(method string) bool {
	if method == "" {
		return false
	}
	for i := 0; i < len(method); i++ {
		b := method[i]
		if b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(b)) {
			continue
		}
		return false
	}
	return true
}

func (a *FiberAdapter) ConfigureBoundedIngress(o BoundedIngressOptions) error {
	return a.ingress.configure(o, a.ValidateBoundedIngress)
}
func (a *FiberV3Adapter) ConfigureBoundedIngress(o BoundedIngressOptions) error {
	return a.ingress.configure(o, a.ValidateBoundedIngress)
}

func (a *FiberAdapter) initializeIngress() error {
	_, enabled, err := a.ingress.setup()
	if err != nil || !enabled {
		return err
	}
	if a.app.Config().Prefork {
		return ingressConfigurationError("native.prefork", "unsupported")
	}
	if err := a.router.ownership.validate(); err != nil {
		return err
	}
	a.router.ownership.before()
	native := a.app.Handler()
	a.router.ownership.afterPrepare()
	return a.ingress.install(a.app.Server(), native, a.app.Config().RequestMethods, a.router.ownership, a.ingressBridge)
}
func (a *FiberV3Adapter) initializeIngress() error {
	_, enabled, err := a.ingress.setup()
	if err != nil || !enabled {
		return err
	}
	if a.listenConfig.EnablePrefork {
		return ingressConfigurationError("native.prefork", "unsupported")
	}
	// Qualification is deliberately limited to default contexts. This setup-only
	// acquisition never participates in unknown-method classification.
	c := a.app.AcquireCtx(&fasthttp.RequestCtx{})
	_, supported := c.(*f3.DefaultCtx)
	a.app.ReleaseCtx(c)
	if !supported {
		return ingressConfigurationError("native.context", "unsupported")
	}
	if err := a.router.ownership.validate(); err != nil {
		return err
	}
	a.router.ownership.before()
	native := a.app.Handler()
	a.router.ownership.afterPrepare()
	return a.ingress.install(a.app.Server(), native, a.app.Config().RequestMethods, a.router.ownership, a.ingressBridge)
}
func (a *FiberAdapter) ingressBridge(c *fasthttp.RequestCtx, ctx context.Context, next fasthttp.RequestHandler) {
	native := a.app.AcquireCtx(c)
	native.SetUserContext(ctx)
	defer a.app.ReleaseCtx(native)
	next(c)
}
func (a *FiberV3Adapter) ingressBridge(c *fasthttp.RequestCtx, ctx context.Context, next fasthttp.RequestHandler) {
	native := f3.NewDefaultCtx(a.app)
	native.Reset(c)
	native.SetContext(ctx)
	// This is an owned bridge, not an app-pool context. Native dispatch owns its
	// separate acquired context and may clear the standard-context slot on release.
	next(c)
}
