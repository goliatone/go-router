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

func DefaultViewEngine(cfg ViewConfigProvider) (Views, error) {
	if err := ValidateConfig(cfg); err != nil {
		return nil, fmt.Errorf("view engine config validation failed: %w", err)
	}

	var viewEngine fiber.Views

	sources := make([]fs.FS, 0)

	// add dev directory w highest priority
	if dcfg, ok := cfg.(interface{ GetDevDir() string }); ok {
		devDir := dcfg.GetDevDir()
		if devDir != "" {
			absDevDir, err := filepath.Abs(devDir)
			if err == nil && DirExists(absDevDir) {
				if cfg.GetDebug() {
					log.Printf("Using dev directory: %s", absDevDir)
				}
				sources = append(sources, os.DirFS(absDevDir))
			} else if cfg.GetDebug() {
				log.Printf("Dev directory not found or accessible: %s", devDir)
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

	if cfg.GetDebug() {
		log.Println("Available templates:")
		entries, _ := fs.ReadDir(compositeFS, NormalizePath(cfg.GetDirFS()))
		for _, entry := range entries {
			log.Printf("  - %s\n", NormalizePath(cfg.GetDirFS())+"/"+entry.Name())
		}

		DebugAssetPaths(compositeFS, "Composite FS")
	}

	engine := django.NewPathForwardingFileSystem(
		http.FS(compositeFS),
		NormalizePath(cfg.GetDirFS()),
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
func InitializeViewEngine(opts ViewConfigProvider) (Views, error) {
	var err error
	var viewEngine fiber.Views

	viewEngine, err = DefaultViewEngine(opts)
	if err != nil {
		return nil, fmt.Errorf("error initializing views: %w", err)
	}

	d, ok := viewEngine.(interface {
		AddFunc(name string, fn interface{}) ftpl.IEngineCore
	})
	if !ok {
		return nil, fmt.Errorf("unexpected view engine type: %T", viewEngine)
	}

	fmt.Println("=========== VIEW INIT =============")
	fmt.Println("HERE WE ARE RUNING...")
	fmt.Println("===================================")

	assetsFs := opts.GetAssetsFS()
	if !opts.GetEmbed() {
		assetsFs = os.DirFS(opts.GetAssetsDir())
	}

	if opts.GetDebug() {
		DebugAssetPaths(assetsFs, "Assets FS")
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
				log.Printf("CSS: filename=%s, path=%s, urlPath=%s, match=%t",
					filename, path, urlPath, g.Match(filename))
			}

			if g.Match(filename) {
				res = template.HTML("<link rel=\"stylesheet\" href=\"" + urlPath + "\">")
				return filepath.SkipDir
			}
			return nil
		}

		if opts.GetDebug() {
			log.Printf("Looking for CSS in embedded path: %s", cssPath)
		}

		fs.WalkDir(assetsFs, cssPath, func(path string, info fs.DirEntry, err error) error {
			return matcher(path, info, err)
		})

		if res == "" && opts.GetDebug() {
			res = template.HTML("<!-- CSS NOT FOUND: " + name + " (looked in " + cssPath + ") -->")
			log.Printf("WARNING: Could not resolve CSS: %s", name)
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
				log.Printf("JS: filename=%s, path=%s, urlPath=%s, match=%t",
					filename, path, urlPath, g.Match(filename))
			}

			if g.Match(filename) {
				res = template.HTML("<script async src=\"" + urlPath + "\"></script>")
				return filepath.SkipDir
			}
			return nil
		}

		if opts.GetDebug() {
			log.Printf("Looking for JS in embedded path: %s", jsPath)
		}

		fs.WalkDir(assetsFs, jsPath, func(path string, info fs.DirEntry, err error) error {
			return matcher(path, info, err)
		})

		if res == "" && opts.GetDebug() {
			res = template.HTML("<!-- JS NOT FOUND: " + name + " (looked in " + jsPath + ") -->")
			log.Printf("WARNING: Could not resolve JS: %s", name)
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
func DebugAssetPaths(dir fs.FS, labels ...string) {
	label := "Asset"
	if len(labels) > 0 {
		label = labels[0]
	}
	fmt.Printf("=== Available %s Paths ===\n", label)

	fs.WalkDir(dir, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			fmt.Println("  - ", path)
		}
		return nil
	})

	fmt.Println("============================")
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
