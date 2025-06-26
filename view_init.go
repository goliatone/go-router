package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"
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

// ViewConfigProvider remains the public configuration interface.
type ViewConfigProvider interface {
	GetReload() bool
	GetDebug() bool
	GetEmbed() bool

	GetCSSPath() string
	GetJSPath() string
	GetDirFS() string
	GetDirOS() string

	GetRemovePathPrefix() string
	GetTemplateFunctions() map[string]any

	GetExt() string

	GetAssetsFS() fs.FS
	GetAssetsDir() string
	GetTemplatesFS() []fs.FS
}

func DefaultViewEngine(cfg ViewConfigProvider, lgrs ...Logger) (fiber.Views, error) {
	if err := ValidateConfig(cfg); err != nil {
		return nil, fmt.Errorf("view engine config validation failed: %w", err)
	}

	lgr := getLogger(lgrs...)
	lgr.Debug("Initializing view engine...")

	var finalTemplateFS fs.FS

	if cfg.GetEmbed() {
		lgr.Debug("Running in Embedded Mode")
		templateSources := make([]fs.FS, 0)

		if dcfg, ok := cfg.(interface{ GetDevDir() string }); ok {
			devDir := dcfg.GetDevDir()
			if devDir != "" {
				absDevDir, err := filepath.Abs(devDir)
				if err == nil && DirExists(absDevDir) {
					lgr.Debug("Adding development override directory for templates", "dir", absDevDir)
					templateSources = append(templateSources, os.DirFS(absDevDir))
				}
			}
		}

		if len(cfg.GetTemplatesFS()) > 0 {
			templateSources = append(templateSources, cfg.GetTemplatesFS()...)
		}

		if len(templateSources) == 0 {
			return nil, errors.New("no valid template sources found for embed mode")
		}

		compositeTemplateFS := cfs.NewCompositeFS(templateSources...)
		templateRootPath := NormalizePath(cfg.GetDirFS())

		subFS, err := autoSubFS(compositeTemplateFS, templateRootPath, lgr)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare embedded template filesystem: %w", err)
		}
		finalTemplateFS = subFS

	} else {
		// --- LIVE MODE LOGIC ---
		lgr.Debug("Running in Live (Non-Embedded) Mode")
		dirOS := cfg.GetDirOS()
		if !DirExists(dirOS) {
			return nil, fmt.Errorf("template directory for live mode does not exist: %s", dirOS)
		}
		lgr.Debug("Using live template directory", "path", dirOS)

		finalTemplateFS = os.DirFS(dirOS)
	}

	engine := django.NewPathForwardingFileSystem(
		http.FS(finalTemplateFS),
		".", // always use root of the prepared filesystem
		cfg.GetExt(),
	)

	pongo2.DefaultSet.Options.TrimBlocks = true
	engine.Reload(cfg.GetReload())
	engine.Debug(cfg.GetDebug())
	engine.AddFuncMap(cfg.GetTemplateFunctions())

	if cfg.GetDebug() {
		lgr.Debug("View engine templates loaded from clean root.")
		fs.WalkDir(finalTemplateFS, ".", func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				lgr.Debug("  - " + path)
			}
			return nil
		})
	}

	return engine, nil
}

func InitializeViewEngine(opts ViewConfigProvider, lgrs ...Logger) (fiber.Views, error) {
	lgr := getLogger(lgrs...)

	viewEngine, err := DefaultViewEngine(opts, lgr)
	if err != nil {
		return nil, fmt.Errorf("error initializing default view engine: %w", err)
	}

	d, ok := viewEngine.(interface {
		AddFunc(name string, fn any) ftpl.IEngineCore
	})
	if !ok {
		return nil, fmt.Errorf("view engine of type %T does not support AddFunc", viewEngine)
	}

	var finalAssetFS fs.FS
	if !opts.GetEmbed() {
		// For non-embedded mode, we deal with absolute OS paths.
		// Use filepath.Clean, not NormalizePath.
		assetRootPath := filepath.Clean(opts.GetAssetsDir())
		if !DirExists(assetRootPath) {
			return nil, fmt.Errorf("asset directory does not exist: %s", assetRootPath)
		}
		finalAssetFS = os.DirFS(assetRootPath)
	} else {
		// For embedded mode, NormalizePath is correct for virtual FS paths.
		assetRootPath := NormalizePath(opts.GetAssetsDir())
		if opts.GetAssetsFS() == nil {
			return nil, errors.New("AssetFS must be provided in embed mode")
		}
		subFS, err := autoSubFS(opts.GetAssetsFS(), assetRootPath, lgr)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare asset filesystem: %w", err)
		}
		finalAssetFS = subFS
	}

	cssPath := NormalizePath(opts.GetCSSPath())
	jsPath := NormalizePath(opts.GetJSPath())

	if !DirExists(cssPath, finalAssetFS) {
		return nil, fmt.Errorf("init view: CSS directory '%s' does not exist within the asset filesystem", cssPath)
	}
	if !DirExists(jsPath, finalAssetFS) {
		return nil, fmt.Errorf("init view: JS directory '%s' does not exist within the asset filesystem", jsPath)
	}

	assetURLPrefix := "/" + NormalizePath(opts.GetRemovePathPrefix())
	if assetURLPrefix == "/" {
		assetURLPrefix = ""
	}

	lgr.Debug("Asset URL prefix computed", "prefix", assetURLPrefix)

	d.AddFunc("css", func(name string) template.HTML {
		var res template.HTML
		g := glob.MustCompile(name)

		fs.WalkDir(finalAssetFS, cssPath, func(path string, info fs.DirEntry, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if g.Match(info.Name()) {
				urlPath := assetURLPrefix + "/" + path
				urlPath = ppath.Clean(urlPath)
				res = template.HTML(`<link rel="stylesheet" href="` + urlPath + `">`)
				lgr.Debug("Resolved CSS asset", "name", name, "path", urlPath)
				return filepath.SkipDir
			}
			return nil
		})

		if res == "" && opts.GetDebug() {
			res = template.HTML("<!-- CSS NOT FOUND: " + name + " (looked in " + cssPath + ") -->")
			lgr.Warn("Could not resolve CSS", "name", name)
		}
		return res
	})

	d.AddFunc("js", func(name string) template.HTML {
		var res template.HTML
		g := glob.MustCompile(name)

		fs.WalkDir(finalAssetFS, jsPath, func(path string, info fs.DirEntry, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if g.Match(info.Name()) {
				urlPath := assetURLPrefix + "/" + path
				urlPath = ppath.Clean(urlPath)
				res = template.HTML(`<script async src="` + urlPath + `"></script>`)
				lgr.Debug("Resolved JS asset", "name", name, "path", urlPath)
				return filepath.SkipDir
			}
			return nil
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
	d.AddFunc("either", eitherCmp)

	return viewEngine, nil
}

// autoSubFS is a key helper function. It inspects an fs.FS to find a
// subdirectory and returns a new fs.FS rooted at that directory.
// This is crucial for making embedded filesystems work intuitively.
func autoSubFS(rootfs fs.FS, path string, lgr Logger) (fs.FS, error) {
	path = NormalizePath(path)
	if path == "" || path == "." {
		lgr.Debug("Filesystem path is root, using as is.")
		return rootfs, nil
	}

	if _, err := fs.Stat(rootfs, path); err == nil {
		lgr.Debug("Found sub-directory in filesystem, creating sub-FS", "path", path)
		return fs.Sub(rootfs, path)
	}

	lgr.Debug("Sub-directory not found, assuming filesystem is already correctly rooted", "path", path)
	return rootfs, nil
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
		return t, errors.New("Can't parse string as time: " + str)
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
		if !t {
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
		if !t {
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

func ValidateConfig(cfg ViewConfigProvider) error {
	var errors []string

	if cfg.GetEmbed() {
		if strings.HasPrefix(cfg.GetCSSPath(), "/") {
			errors = append(errors, "CSS path should not start with '/' when embed is true")
		}
		if strings.HasPrefix(cfg.GetJSPath(), "/") {
			errors = append(errors, "JS path should not start with '/' when embed is true")
		}
		if cfg.GetTemplatesFS() == nil {
			errors = append(errors, "No template filesystems provided when embed is true")
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

func DebugAssetPaths(lgr Logger, dir fs.FS, labels ...string) {
	label := "Asset"
	if len(labels) > 0 {
		label = labels[0]
	}
	lgr.Debug(fmt.Sprintf("=== Available Paths in %s ===", label))

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

func NormalizePath(path string) string {
	path = ppath.Clean(path)
	path = filepath.ToSlash(path)
	path = strings.Trim(path, "/")

	if path == "." {
		return ""
	}
	return path
}

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

func DirExists(path string, afs ...fs.FS) bool {
	var err error
	var info fs.FileInfo

	if len(afs) > 0 {
		info, err = fs.Stat(afs[0], path)
	} else {
		info, err = os.Stat(path)
	}

	if err != nil {
		return false
	}
	return info.IsDir()
}

func getLogger(lgrs ...Logger) Logger {
	if len(lgrs) > 0 && lgrs[0] != nil {
		return lgrs[0]
	}
	return &defaultLogger{}
}
