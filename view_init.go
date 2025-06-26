package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	ppath "path"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/flosch/pongo2/v6"
	"github.com/gobwas/glob"
	"github.com/gofiber/fiber/v2"
	ftpl "github.com/gofiber/template"
	"github.com/gofiber/template/django/v3"
	cfs "github.com/goliatone/go-composite-fs"
	"github.com/goodsign/monday"
)

type nameable interface {
	Name() string
}

type ViewConfigProvider interface {
	GetReload() bool
	GetDebug() bool
	GetEmbed() bool

	GetCSSPath() string
	GetJSPath() string
	// GetDevDir() string
	GetDirFS() string
	GetDirOS() string

	GetRemovePathPrefix() string
	GetTemplateFunctions() map[string]any

	GetExt() string

	GetAssetsFS() fs.FS
	GetAssetsDir() string
	GetTemplatesFS() []fs.FS
}

type ViewFactory func(ViewConfigProvider) (Views, error)

func DefaultViewEngine(cfg ViewConfigProvider, lgrs ...Logger) (Views, error) {
	if err := ValidateConfig(cfg); err != nil {
		return nil, fmt.Errorf("view engine config validation failed: %w", err)
	}

	lgr := getLogger(lgrs...)

	var viewEngine fiber.Views

	sources := make([]fs.FS, 0)

	// add dev directory w highest priority
	if dcfg, ok := cfg.(interface{ GetDevDir() string }); ok {
		devDir := dcfg.GetDevDir()
		if devDir != "" {
			absDevDir, err := filepath.Abs(devDir)
			if err == nil && DirExists(absDevDir) {
				if cfg.GetDebug() {
					lgr.Debug("Using dev directory", "dir", absDevDir)
				}
				sources = append(sources, os.DirFS(absDevDir))
			} else if cfg.GetDebug() {
				lgr.Debug("Dev directory not found or accessible", "dir", devDir)
			}
		}
	}

	if cfg.GetEmbed() {
		// add all embedded fs with priority order
		embeddedSources := cfg.GetTemplatesFS()
		if len(embeddedSources) > 0 {
			sources = append(sources, embeddedSources...)
		}
	} else {
		dirOS := cfg.GetDirOS()
		if !DirExists(dirOS) {
			return nil, fmt.Errorf("template directory does not exist: %s", dirOS)
		}
		sources = append(sources, os.DirFS(dirOS))
	}

	if len(sources) == 0 {
		return nil, fmt.Errorf("no valid template sources found")
	}

	compositeFS := cfs.NewCompositeFS(sources...)
	templatePathPrefix := NormalizePath(cfg.GetDirFS())

	if templatePathPrefix != "" && templatePathPrefix != "." {
		if _, err := fs.Stat(compositeFS, templatePathPrefix); err != nil {
			errMsg := fmt.Sprintf(
				"template path prefix '%s' (from dir_fs config) not found in any of the configured template sources. Error: %v",
				templatePathPrefix,
				err,
			)
			if cfg.GetDebug() {
				lgr.Debug("Root entries of the composite filesystem:")
				fs.WalkDir(compositeFS, ".", func(path string, d fs.DirEntry, err error) error {
					if err == nil {
						lgr.Debug(fmt.Sprintf("  - %s", path))
					}
					return nil
				})
			}
			return nil, errors.New(errMsg)
		}
	}

	if cfg.GetDebug() {
		lgr.Debug("Available templates (full paths in composite FS)")
		fs.WalkDir(compositeFS, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() {
				lgr.Debug("  - " + path)
			}
			return nil
		})
	}

	pongo2.DefaultSet.Options.TrimBlocks = true
	engine := django.NewPathForwardingFileSystem(
		http.FS(compositeFS),
		templatePathPrefix,
		cfg.GetExt(),
	)

	viewEngine = engine

	if engine, ok := viewEngine.(*django.Engine); ok {
		engine.Reload(cfg.GetReload())
		engine.Debug(cfg.GetDebug())
		engine.AddFuncMap(cfg.GetTemplateFunctions())
	}

	return viewEngine, nil
}

// InitializeViewEngine will initialize a view engine with default values
func InitializeViewEngine(opts ViewConfigProvider, lgrs ...Logger) (Views, error) {
	var err error
	var viewEngine fiber.Views

	lgr := getLogger(lgrs...)

	viewEngine, err = DefaultViewEngine(opts, lgr)
	if err != nil {
		return nil, fmt.Errorf("error initializing views: %w", err)
	}

	d, ok := viewEngine.(interface {
		AddFunc(name string, fn any) ftpl.IEngineCore
	})
	if !ok {
		return nil, fmt.Errorf("unexpected view engine type: %T", viewEngine)
	}

	lgr.Debug("=========== VIEW INIT =============")
	lgr.Debug("HERE WE ARE RUNING...")
	lgr.Debug("===================================")

	assetsFs := opts.GetAssetsFS()
	if !opts.GetEmbed() {
		assetsFs = os.DirFS(opts.GetAssetsDir())
	}

	if opts.GetDebug() {
		DebugAssetPaths(lgr, assetsFs, "Assets FS")
	}

	assetPrefix := NormalizePath(opts.GetRemovePathPrefix())

	jsPath := NormalizePath(opts.GetJSPath())
	cssPath := NormalizePath(opts.GetCSSPath())

	if !DirExists(jsPath, assetsFs) {
		jsPath = path.Join(assetPrefix, NormalizePath(opts.GetJSPath()))
		if !DirExists(jsPath, assetsFs) {
			return nil, errors.New("init view: JS directory does not exist: " + jsPath)
		}
	}

	if !DirExists(cssPath, assetsFs) {
		cssPath = path.Join(assetPrefix, NormalizePath(opts.GetCSSPath()))
		if !DirExists(cssPath, assetsFs) {
			return nil, errors.New("init view: CSS directory does not exist: " + cssPath)
		}
	}

	d.AddFunc("css", func(name string) template.HTML {
		var res template.HTML
		g := glob.MustCompile(name)

		matcher := func(path string, info nameable, err error) error {
			if err != nil {
				if opts.GetDebug() {
					lgr.Error("Error accessing path", "path", path, err)
				}
				return nil
			}

			filename := info.Name()
			path = filepath.ToSlash(path)

			urlPath := path
			if assetPrefix != "" && strings.HasPrefix(urlPath, assetPrefix) {
				urlPath = strings.Replace(urlPath, assetPrefix, "", 1)
			}

			urlPath = "/" + strings.TrimPrefix(urlPath, "/")

			if opts.GetDebug() {
				lgr.Debug("CSS files",
					"filename", filename,
					"path", path,
					"url_path", urlPath,
					"match", g.Match(filename),
				)
			}

			if g.Match(filename) {
				res = template.HTML("<link rel=\"stylesheet\" href=\"" + urlPath + "\">")
				return filepath.SkipDir
			}
			return nil
		}

		if opts.GetDebug() {
			lgr.Debug("Looking for CSS in embedded path", "path", cssPath)
		}

		fs.WalkDir(assetsFs, cssPath, func(path string, info fs.DirEntry, err error) error {
			return matcher(path, info, err)
		})

		if res == "" && opts.GetDebug() {
			res = template.HTML("<!-- CSS NOT FOUND: " + name + " (looked in " + cssPath + ") -->")
			lgr.Warn("Could not resolve CSS: %s", "name", name)
		}

		return res
	})

	// TODO: take options https://developer.mozilla.org/en-US/docs/Web/HTML/Element/script
	d.AddFunc("js", func(name string) template.HTML {
		var res template.HTML
		g := glob.MustCompile(name)

		matcher := func(path string, info nameable, err error) error {
			if err != nil {
				if opts.GetDebug() {
					log.Printf("Error accessing path %s: %v", path, err)
				}
				return nil
			}

			filename := info.Name()
			path = filepath.ToSlash(path)

			urlPath := path
			if assetPrefix != "" && strings.HasPrefix(urlPath, assetPrefix) {
				urlPath = strings.Replace(urlPath, assetPrefix, "", 1)
			}

			urlPath = "/" + strings.TrimPrefix(urlPath, "/")

			if opts.GetDebug() {
				lgr.Debug("JS",
					"filename", filename,
					"path", path,
					"url_path", urlPath,
					"match", g.Match(filename),
				)
			}

			if g.Match(filename) {
				res = template.HTML("<script async src=\"" + urlPath + "\"></script>")
				return filepath.SkipDir
			}
			return nil
		}

		if opts.GetDebug() {
			lgr.Debug("Looking for JS in embedded path", "js_path", jsPath)
		}

		fs.WalkDir(assetsFs, jsPath, func(path string, info fs.DirEntry, err error) error {
			return matcher(path, info, err)
		})

		if res == "" && opts.GetDebug() {
			res = template.HTML("<!-- JS NOT FOUND: " + name + " (looked in " + jsPath + ") -->")
			lgr.Warn("Could not resolve JS", "name", name)
		}

		return res
	})

	d.AddFunc("to_json", toJSON)

	d.AddFunc("match_str", matchStr)

	d.AddFunc("str_time", makeTimeParser())

	d.AddFunc("conditional_str", conditionalStr)

	d.AddFunc("match_str", matchStr)

	d.AddFunc("either", eitherCmp)

	return viewEngine, nil
}

func makeTimeParser() func(val any, format string, lang string) string {
	var TimeFormats = []string{
		"2006", "2006-1", "2006-1-2", "2006-1-2 15", "2006-1-2 15:4", "2006-1-2 15:4:5", "1-2",
		"15:4:5", "15:4", "15",
		"15:4:5 Jan 2, 2006 MST", "2006-01-02 15:04:05.999999999 -0700 MST", "2006-01-02T15:04:05Z0700", "2006-01-02T15:04:05Z07",
		"2006.1.2", "2006.1.2 15:04:05", "2006.01.02", "2006.01.02 15:04:05", "2006.01.02 15:04:05.999999999",
		"1/2/2006", "1/2/2006 15:4:5", "2006/01/02", "20060102", "2006/01/02 15:04:05",
		time.ANSIC, time.UnixDate, time.RubyDate, time.RFC822, time.RFC822Z, time.RFC850,
		time.RFC1123, time.RFC1123Z, time.RFC3339, time.RFC3339Nano,
		time.Kitchen, time.Stamp, time.StampMilli, time.StampMicro, time.StampNano,
	}

	parseWithFormat := func(str string) (t time.Time, err error) {
		for _, format := range TimeFormats {
			t, err = time.ParseInLocation(format, str, time.Local)

			if err == nil {
				return t, err
			}
		}

		err = errors.New("Can't parse string as time: " + str)

		return t, err
	}

	return func(val any, format string, lang string) string {
		if val == nil {
			return ""
		}

		date, ok := val.(string)
		if !ok {
			return ""
		}

		d, err := parseWithFormat(date)
		if err != nil {
			fmt.Printf("error str_time: %s\n", err)
			return ""
		}
		var mloc monday.Locale
		mloc = monday.LocaleEnUS
		if lang == "es" {
			mloc = monday.LocaleEsES
		}

		return monday.Format(d, format, mloc)
	}
}

func eitherCmp(val any, def any) any {
	if val == nil {
		return def
	}

	switch t := val.(type) {
	case string:
		if t == "" {
			return def
		}
	case bool:
		if t == false {
			return def
		}
	case int, int8, int16, int32, int64:
		if t == 0 {
			return def
		}
	case uint, uint8, uint16, uint32, uint64:
		if t == 0 {
			return def
		}
	case float32, float64:
		if t == 0 {
			return def
		}
	default:
		v := reflect.ValueOf(t)
		if !v.IsValid() || reflect.DeepEqual(v.Interface(), reflect.Zero(v.Type()).Interface()) {
			return def
		}
	}

	return val
}

func toJSON(data any) string {
	b, err := json.MarshalIndent(data, "", "    ")
	if err != nil {
		return ""
	}
	return string(b)
}

func matchStr(a, b string, ok, ko string) string {
	if a == b {
		return ok
	}
	return ko
}

func conditionalStr(arg any, ok, ko string) string {
	if arg == nil {
		return ko
	}

	switch t := arg.(type) {
	case string:
		if t == "" {
			return ko
		}
	case bool:
		if t == false {
			return ko
		}
	case int, int8, int16, int32, int64:
		if t == 0 {
			return ko
		}
	case uint, uint8, uint16, uint32, uint64:
		if t == 0 {
			return ko
		}
	case float32, float64:
		if t == 0 {
			return ko
		}
	default:
		v := reflect.ValueOf(t)
		if !v.IsValid() || reflect.DeepEqual(v.Interface(), reflect.Zero(v.Type()).Interface()) {
			return ko
		}
	}

	return ok
}

// ValidateConfig checks for common configuration errors
func ValidateConfig(cfg ViewConfigProvider) error {
	errors := []string{}

	if cfg.GetEmbed() {
		if strings.HasPrefix(cfg.GetCSSPath(), "/") {
			errors = append(errors, "CSS path should not start with '/' when embed is true")
		}

		if strings.HasPrefix(cfg.GetJSPath(), "/") {
			errors = append(errors, "JS path should not start with '/' when embed is true")
		}

		if cfg.GetTemplatesFS() == nil {
			errors = append(errors, "No template filesystems provided with embed is true")
		}
	} else {
		if _, err := os.Stat(cfg.GetDirOS()); os.IsNotExist(err) {
			errors = append(errors, fmt.Sprintf("Template directory '%s' does not exist", cfg.GetDirOS()))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("Configuration errors:\n- %s", strings.Join(errors, "\n- "))
	}

	return nil
}

// DebugAssetPaths prints all available assets for debugging
func DebugAssetPaths(lgr Logger, dir fs.FS, labels ...string) {
	label := "Asset"
	if len(labels) > 0 {
		label = labels[0]
	}
	lgr.Debug("=== Available Paths ===", "label", label)

	fs.WalkDir(dir, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			lgr.Debug("  - " + path)
		}
		return nil
	})

	lgr.Debug("============================")
}

// NormalizePath ensures consistent path formatting
func NormalizePath(path string) string {
	path = ppath.Clean(path)
	path = filepath.ToSlash(path)
	path = strings.TrimPrefix(path, "/")

	// remove trailing slash if present unless it's the root path
	if path != "" {
		path = strings.TrimSuffix(path, "/")
	}

	if path == "." {
		path = ""
	}

	return path
}

// ResolvePath combines base paths with subdirectories properly
func ResolvePath(base, subPath string) string {
	base = NormalizePath(base)
	subPath = NormalizePath(subPath)

	if base == "" {
		return subPath
	}

	if subPath == "" {
		return base
	}

	return base + "/" + subPath
}

// DirExists checks if a directory exists and is accessible
func DirExists(path string, afs ...fs.FS) bool {
	var err error
	var info os.FileInfo

	if len(afs) > 0 {
		info, err = fs.Stat(afs[0], path)
	} else {
		info, err = os.Stat(path)
	}

	if err != nil {
		return false // prob doesnt exist or perms errors?
	}

	return info.IsDir()
}

func getLogger(lgrs ...Logger) Logger {
	if len(lgrs) > 0 {
		return lgrs[0]
	}

	return &defaultLogger{}
}
