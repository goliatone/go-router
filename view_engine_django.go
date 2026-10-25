package router

import (
	"fmt"
	"io"
	"log"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"reflect"

	"github.com/flosch/pongo2/v7"
	core "github.com/gofiber/template/v2"
)

// fiberV3DjangoViewEngine uses per-template-set escaping. It cannot mutate the
// legacy Django engine's pongo2/v6 globals. Published template snapshots are
// immutable; no engine lock is held while user template functions execute.
type fiberV3DjangoViewEngine struct {
	core.Engine
	autoescape bool
	templates  map[string]*pongo2.Template
	filters    map[string]pongo2.FilterFunction
	tags       map[string]pongo2.TagParser
}

var _ core.IEngine = (*fiberV3DjangoViewEngine)(nil)

func (e *fiberV3DjangoViewEngine) SetAutoEscape(enabled bool) {
	e.Mutex.Lock()
	defer e.Mutex.Unlock()
	if e.autoescape != enabled {
		e.autoescape = enabled
		e.Loaded = false
	}
}

func (e *fiberV3DjangoViewEngine) Load() error {
	e.Mutex.Lock()
	defer e.Mutex.Unlock()
	return e.loadLocked()
}

func (e *fiberV3DjangoViewEngine) loadLocked() error {
	e.Loaded = false
	loader, err := e.templateLoader()
	if err != nil {
		return err
	}
	set := pongo2.NewSet("fiber-v3", loader)
	set.SetAutoescape(e.autoescape)
	for name, filter := range e.filters {
		if registerErr := set.RegisterFilter(name, filter); registerErr != nil {
			return registerErr
		}
	}
	for name, parser := range e.tags {
		if registerErr := set.RegisterTag(name, parser); registerErr != nil {
			return registerErr
		}
	}
	globals, err := fiberV3ViewValue(e.Funcmap, "globals")
	if err != nil {
		return err
	}
	globalValues, ok := globals.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid template globals type %T", globals)
	}
	set.Globals.Update(globalValues)
	templates := make(map[string]*pongo2.Template)
	walk := func(path string, info os.FileInfo, walkErr error) error {
		return e.parseTemplate(set, templates, path, info, walkErr)
	}
	if e.FileSystem != nil {
		err = core.Walk(e.FileSystem, e.Directory, walk)
	} else {
		err = filepath.Walk(e.Directory, walk)
	}
	if err != nil {
		return err
	}
	e.templates = templates
	e.Loaded = true
	return nil
}

// Preserve the native Django HTTP-filesystem loader's root-relative include
// behavior, including the shared extension-aware fallback filesystem.
type fiberV3DjangoFSLoader struct {
	fs        http.FileSystem
	directory string
}

func (l fiberV3DjangoFSLoader) Abs(_, name string) string { return name }

func (l fiberV3DjangoFSLoader) Get(name string) (io.Reader, error) {
	return l.fs.Open(filepath.Join(l.directory, name))
}

func (e *fiberV3DjangoViewEngine) Render(out io.Writer, name string, binding any, layouts ...string) error {
	tmpl, layout, layoutName, err := e.renderSnapshot(name, layouts)
	if err != nil {
		return err
	}
	bind, err := fiberV3DjangoBinding(binding)
	if err != nil {
		return err
	}
	if layout == nil {
		return tmpl.ExecuteWriter(bind, out)
	}
	data, err := tmpl.ExecuteBytes(bind)
	if err != nil {
		return err
	}
	// Each render owns its layout binding; caller maps and concurrent renders
	// never share the temporary embed key.
	layoutBind := make(pongo2.Context, len(bind)+1)
	maps.Copy(layoutBind, bind)
	layoutBind[layoutName] = pongo2.AsSafeValue(string(data))
	return layout.ExecuteWriter(layoutBind, out)
}

func (e *fiberV3DjangoViewEngine) renderSnapshot(name string, layouts []string) (*pongo2.Template, *pongo2.Template, string, error) {
	e.Mutex.Lock()
	defer e.Mutex.Unlock()
	if !e.Loaded || e.ShouldReload {
		if err := e.loadLocked(); err != nil {
			return nil, nil, "", err
		}
	}
	tmpl := e.templates[name]
	if tmpl == nil {
		return nil, nil, "", fmt.Errorf("template %s does not exist", name)
	}
	var layout *pongo2.Template
	if len(layouts) > 0 && layouts[0] != "" {
		layout = e.templates[layouts[0]]
		if layout == nil {
			return nil, nil, "", fmt.Errorf("LayoutName %s does not exist", layouts[0])
		}
	}
	return tmpl, layout, e.LayoutName, nil
}

// Match the v3 template core's named-string-map support and identifier filtering
// without mutating caller data (including layout and buffered-render bindings).
func fiberV3DjangoBinding(binding any) (pongo2.Context, error) {
	value := reflect.ValueOf(binding)
	if !value.IsValid() || value.Kind() != reflect.Map || value.Type().Key().Kind() != reflect.String || value.IsNil() {
		return nil, nil
	}
	bind := make(pongo2.Context, value.Len())
	for _, key := range value.MapKeys() {
		name := key.String()

		if fiberV3BindingIdentifier(name) {
			item, err := fiberV3ViewValue(value.MapIndex(key).Interface(), "binding."+name)
			if err != nil {
				return nil, err
			}
			bind[name] = item
		}
	}
	return bind, nil
}

func (e *fiberV3DjangoViewEngine) templateLoader() (pongo2.TemplateLoader, error) {
	if e.FileSystem != nil {
		return fiberV3DjangoFSLoader{fs: e.FileSystem, directory: e.Directory}, nil
	}
	return pongo2.NewLocalFileSystemLoader(e.Directory)
}

func (e *fiberV3DjangoViewEngine) parseTemplate(set *pongo2.TemplateSet, templates map[string]*pongo2.Template, path string, info os.FileInfo, walkErr error) error {
	if walkErr != nil {
		return walkErr
	}
	if info == nil || info.IsDir() || !core.HasExtension(path, e.Extension) {
		return nil
	}
	name, err := core.TemplateName(e.Directory, path, e.Extension)
	if err != nil {
		return err
	}
	data, err := core.ReadFile(path, e.FileSystem)
	if err != nil {
		return err
	}
	tmpl, err := set.FromBytes(data)
	if err != nil {
		return err
	}
	templates[name] = tmpl
	if e.Verbose {
		log.Printf("views: parsed template: %s", name)
	}
	return nil
}

func fiberV3BindingIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for _, ch := range name {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' {
			continue
		}
		return false
	}
	return true
}
