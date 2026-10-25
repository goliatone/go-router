package router

import (
	"fmt"
	"net/http"
	"strings"

	fiber3 "github.com/gofiber/fiber/v3"
	core2 "github.com/gofiber/template/v2"
)

// FiberV3ViewConfig is an optional ViewConfigProvider capability for registering
// v7 ports of custom filters and tags when a v3 engine is created. The v2 helper
// ignores this capability. Registrations survive v3 template loads and reloads.
type FiberV3ViewConfig interface {
	ConfigureFiberV3Views(FiberV3ViewExtensions) error
}

// FiberV3ViewFunctionsProvider optionally supplies v3-specific function overrides
// without changing ViewConfigProvider. Overrides win over shared/generated
// helpers; legacy view constructors ignore this interface.
type FiberV3ViewFunctionsProvider interface {
	GetFiberV3TemplateFunctions() map[string]any
}

func applyFiberV3ViewFunctions(engine interface {
	AddFuncMap(map[string]any) core2.IEngineCore
}, cfg ViewConfigProvider) {
	if provider, ok := cfg.(FiberV3ViewFunctionsProvider); ok {
		engine.AddFuncMap(provider.GetFiberV3TemplateFunctions())
	}
}

// DefaultFiberV3ViewEngine creates a v3-compatible Django engine with the same
// filesystem selection rules as DefaultViewEngine.
func DefaultFiberV3ViewEngine(cfg ViewConfigProvider, lgrs ...Logger) (fiber3.Views, error) {
	lgr := getLogger(lgrs...)
	finalTemplateFS, err := prepareViewTemplateFS(cfg, lgr)
	if err != nil {
		return nil, err
	}
	ext := strings.TrimSpace(cfg.GetExt())
	engine := &fiberV3DjangoViewEngine{autoescape: true}
	engine.Directory = cfg.GetDirOS()
	engine.Extension = ext
	engine.Left, engine.Right = "{{", "}}"
	engine.LayoutName = "embed"
	engine.Funcmap = make(map[string]any)
	if cfg.GetEmbed() {
		engine.FileSystem = http.FS(finalTemplateFS)
		engine.Directory = "."
	}
	engine.Reload(cfg.GetReload())
	engine.Debug(cfg.GetDebug())
	engine.AddFuncMap(cfg.GetTemplateFunctions())
	if config, ok := cfg.(FiberV3ViewConfig); ok {
		if err := config.ConfigureFiberV3Views(engine); err != nil {
			return nil, fmt.Errorf("configure v3 view extensions: %w", err)
		}
	}
	applyFiberV3ViewFunctions(engine, cfg)
	if cfg.GetDebug() {
		DebugAssetPaths(lgr, finalTemplateFS, "template FS")
	}
	return engine, nil
}

// InitializeFiberV3ViewEngine adds the shared template and asset helpers using
// template/v2's IEngineCore, preserving the legacy template core entry points.
func InitializeFiberV3ViewEngine(opts ViewConfigProvider, lgrs ...Logger) (fiber3.Views, error) {
	lgr := getLogger(lgrs...)
	viewEngine, err := DefaultFiberV3ViewEngine(opts, lgr)
	if err != nil {
		return nil, fmt.Errorf("error initializing default v3 view engine: %w", err)
	}
	d, ok := viewEngine.(interface {
		AddFunc(string, any) core2.IEngineCore
		AddFuncMap(map[string]any) core2.IEngineCore
	})
	if !ok {
		return nil, fmt.Errorf("view engine of type %T does not support v3 AddFunc", viewEngine)
	}
	initializeViewHelpers(opts, lgr, func(name string, fn any) { d.AddFunc(name, fn) })
	applyFiberV3ViewFunctions(d, opts)
	return viewEngine, nil
}
