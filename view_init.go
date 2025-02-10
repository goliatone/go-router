package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/gobwas/glob"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/template/django/v3"
	"github.com/goodsign/monday"
)

type ViewConfigProvider interface {
	ViewFactoryConfigProvider

	GetCSSPath() string
	GetJSPath() string
	GetRemovePathPrefix() string
	GetReload() bool
	GetDebug() bool
	GetAssetsFS() fs.FS
	GetTemplateFunctions() map[string]any
}

type ViewFactoryConfigProvider interface {
	GetEmbed() bool
	GetDirFS() string
	GetDirOS() string
	GetExt() string
	GetTemplatesFS() fs.FS
}

type ViewFactory func(ViewFactoryConfigProvider) Views

func DefaultViewEngine(opts ViewFactoryConfigProvider) Views {
	var viewEngine fiber.Views
	if opts.GetEmbed() {
		viewEngine = django.NewPathForwardingFileSystem(
			http.FS(opts.GetTemplatesFS()),
			opts.GetDirFS(),
			opts.GetExt(),
		)
	} else {
		viewEngine = django.New(opts.GetDirOS(), opts.GetExt())
	}
	return viewEngine
}

// InitializeViewEngine will initialize a view engine with default values
func InitializeViewEngine(opts ViewConfigProvider) Views {
	var viewEngine fiber.Views

	viewEngine = DefaultViewEngine(opts)

	d, _ := viewEngine.(*django.Engine)

	d.Reload(opts.GetReload())
	d.Debug(opts.GetDebug())

	d.AddFuncMap(opts.GetTemplateFunctions())

	d.AddFunc("css", func(name string) template.HTML {
		var res template.HTML
		g := glob.MustCompile(name)

		matcher := func(path, name string, err error) error {
			if err != nil {
				return err
			}
			path = filepath.ToSlash(path)
			path = strings.Replace(path, opts.GetRemovePathPrefix(), "", 1)
			if g.Match(name) {
				res = template.HTML("<link rel=\"stylesheet\" href=\"/" + path + "\">")
			}
			return nil
		}
		// TODO: os.DirFS(opts.GetCSSPath())
		if opts.GetEmbed() {
			fs.WalkDir(opts.GetAssetsFS(), opts.GetCSSPath(), func(path string, info fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				return matcher(path, info.Name(), err)
			})
		} else {
			filepath.Walk(opts.GetCSSPath(), func(path string, info fs.FileInfo, err error) error {
				if err != nil {
					return err
				}
				return matcher(path, info.Name(), err)
			})
		}

		return res
	})

	// TODO: take options https://developer.mozilla.org/en-US/docs/Web/HTML/Element/script
	d.AddFunc("js", func(name string) template.HTML {
		var res template.HTML
		g := glob.MustCompile(name)

		matcher := func(path, name string, err error) error {
			if err != nil {
				return err
			}

			path = filepath.ToSlash(path)
			path = strings.Replace(path, opts.GetRemovePathPrefix(), "", 1)

			if g.Match(name) {
				res = template.HTML("<script async src=\"/" + path + "\"></script>")
			}
			return nil
		}
		// TODO: os.DirFS(opts.GetCSSPath())
		if opts.GetEmbed() {
			fs.WalkDir(opts.GetAssetsFS(), opts.GetJSPath(), func(path string, info fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				return matcher(path, info.Name(), err)
			})
		} else {
			filepath.Walk(opts.GetJSPath(), func(path string, info fs.FileInfo, err error) error {
				if err != nil {
					return err
				}
				return matcher(path, info.Name(), err)
			})
		}

		return res
	})

	d.AddFunc("to_json", func(data any) string {
		b, err := json.MarshalIndent(data, "", "    ")
		if err != nil {
			return ""
		}
		return string(b)
	})

	d.AddFunc("match_str", func(a, b string, ok, ko string) string {
		if a == b {
			return ok
		}
		return ko
	})

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

	d.AddFunc("str_time", func(val any, format string, lang string) string {
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
	})

	d.AddFunc("conditional_str", func(arg any, ok, ko string) string {
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
	})

	d.AddFunc("match_str", func(a, b any, ok, ko string) string {
		if a == b {
			return ok
		}
		return ko
	})

	d.AddFunc("either", func(val any, def any) any {
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
	})

	return viewEngine
}
