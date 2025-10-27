package router

import "io/fs"

// SimpleViewConfig provides sensible defaults for file-based templates without
// requiring the full ViewConfigProvider surface. Assets are opt-in.
type SimpleViewConfig struct {
	DirOS      string
	Ext        string
	Reload     bool
	Debug      bool
	URLPrefix  string
	Functions  map[string]any
	AssetsDir  string
	CSSPath    string
	JSPath     string
	TemplateFS []fs.FS
}

// NewSimpleViewConfig initializes a quick-start configuration rooted at dirOS.
// Asset directories are opt-in and disabled unless configured explicitly.
func NewSimpleViewConfig(dirOS string) *SimpleViewConfig {
	return &SimpleViewConfig{
		DirOS:  dirOS,
		Ext:    ".html",
		Reload: true,
		Debug:  true,
	}
}

// WithAssets enables static asset helpers using the provided root directory and
// optional css/js subdirectories.
func (c *SimpleViewConfig) WithAssets(dir string, cssPath string, jsPath string) *SimpleViewConfig {
	c.AssetsDir = dir
	c.CSSPath = cssPath
	c.JSPath = jsPath
	return c
}

// WithExt overrides the default template extension.
func (c *SimpleViewConfig) WithExt(ext string) *SimpleViewConfig {
	c.Ext = ext
	return c
}

// WithReload toggles template reload support.
func (c *SimpleViewConfig) WithReload(reload bool) *SimpleViewConfig {
	c.Reload = reload
	return c
}

// WithDebug toggles debug logging.
func (c *SimpleViewConfig) WithDebug(debug bool) *SimpleViewConfig {
	c.Debug = debug
	return c
}

// WithURLPrefix configures the asset URL prefix.
func (c *SimpleViewConfig) WithURLPrefix(prefix string) *SimpleViewConfig {
	c.URLPrefix = prefix
	return c
}

// WithFunctions registers template helper functions.
func (c *SimpleViewConfig) WithFunctions(funcs map[string]any) *SimpleViewConfig {
	c.Functions = funcs
	return c
}

// ViewConfigProvider implementation

func (c *SimpleViewConfig) GetReload() bool {
	return c.Reload
}

func (c *SimpleViewConfig) GetDebug() bool {
	return c.Debug
}

func (c *SimpleViewConfig) GetEmbed() bool {
	return false
}

func (c *SimpleViewConfig) GetCSSPath() string {
	return c.CSSPath
}

func (c *SimpleViewConfig) GetJSPath() string {
	return c.JSPath
}

func (c *SimpleViewConfig) GetDirFS() string {
	return ""
}

func (c *SimpleViewConfig) GetDirOS() string {
	return c.DirOS
}

func (c *SimpleViewConfig) GetURLPrefix() string {
	return c.URLPrefix
}

func (c *SimpleViewConfig) GetTemplateFunctions() map[string]any {
	return c.Functions
}

func (c *SimpleViewConfig) GetExt() string {
	if c.Ext == "" {
		return ".html"
	}
	return c.Ext
}

func (c *SimpleViewConfig) GetAssetsFS() fs.FS {
	return nil
}

func (c *SimpleViewConfig) GetAssetsDir() string {
	return c.AssetsDir
}

func (c *SimpleViewConfig) GetTemplatesFS() []fs.FS {
	return c.TemplateFS
}
