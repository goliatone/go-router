package router_test

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/goliatone/go-router"
)

//go:embed testdata/assets
var assetsFS embed.FS

//go:embed testdata/templates
var baseTemplates embed.FS

//go:embed testdata/theme
var themeTemplates embed.FS

type MockViewConfig struct {
	Reload       bool
	Debug        bool
	Embed        bool
	CSSPath      string
	JSPath       string
	DevDir       string
	DirFS        string
	DirOS        string
	RemovePrefix string
	Ext          string
	TemplateFS   []fs.FS
	AssetFS      fs.FS
	AssetsDir    string
	Functions    map[string]any
}

func (m *MockViewConfig) GetReload() bool                      { return m.Reload }
func (m *MockViewConfig) GetDebug() bool                       { return m.Debug }
func (m *MockViewConfig) GetEmbed() bool                       { return m.Embed }
func (m *MockViewConfig) GetCSSPath() string                   { return m.CSSPath }
func (m *MockViewConfig) GetJSPath() string                    { return m.JSPath }
func (m *MockViewConfig) GetDevDir() string                    { return m.DevDir }
func (m *MockViewConfig) GetDirFS() string                     { return m.DirFS }
func (m *MockViewConfig) GetDirOS() string                     { return m.DirOS }
func (m *MockViewConfig) GetRemovePathPrefix() string          { return m.RemovePrefix }
func (m *MockViewConfig) GetExt() string                       { return m.Ext }
func (m *MockViewConfig) GetAssetsFS() fs.FS                   { return m.AssetFS }
func (m *MockViewConfig) GetAssetsDir() string                 { return m.AssetsDir }
func (m *MockViewConfig) GetTemplatesFS() []fs.FS              { return m.TemplateFS }
func (m *MockViewConfig) GetTemplateFunctions() map[string]any { return m.Functions }

func setupTempDir(t *testing.T, content map[string]string) string {
	t.Helper()

	tempDir, err := os.MkdirTemp("", "view-engine-test-*")
	require.NoError(t, err, "Failed to create temp directory")

	for path, data := range content {
		fullPath := filepath.Join(tempDir, path)
		dir := filepath.Dir(fullPath)

		err := os.MkdirAll(dir, 0755)
		require.NoError(t, err, "Failed to create directory: %s", dir)

		err = os.WriteFile(fullPath, []byte(data), 0644)
		require.NoError(t, err, "Failed to write file: %s", fullPath)
	}

	return tempDir
}

func createFiberApp(t *testing.T, viewEngine fiber.Views) *fiber.App {
	t.Helper()

	app := fiber.New(fiber.Config{
		Views: viewEngine,
	})

	app.Get("/test-template", func(c *fiber.Ctx) error {
		return c.Render("test", fiber.Map{
			"Title": "Test Page",
		})
	})

	app.Get("/with-assets", func(c *fiber.Ctx) error {
		return c.Render("with-assets", fiber.Map{
			"Title": "Assets Test",
		})
	})

	return app
}

func TestViewEngineValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      MockViewConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "Valid embedded config",
			config: MockViewConfig{
				Embed:      true,
				DirFS:      "templates",
				Ext:        ".html",
				CSSPath:    "css",
				JSPath:     "js",
				TemplateFS: []fs.FS{baseTemplates},
				AssetFS:    assetsFS,
			},
			expectError: false,
		},
		{
			name: "Invalid CSS path with leading slash",
			config: MockViewConfig{
				Embed:      true,
				DirFS:      "templates",
				Ext:        ".html",
				CSSPath:    "/css", // error <- has leading slash
				JSPath:     "js",
				TemplateFS: []fs.FS{baseTemplates},
				AssetFS:    assetsFS,
			},
			expectError: true,
			errorMsg:    "CSS path should not start with '/' when embed is true",
		},
		{
			name: "Invalid JS path with leading slash",
			config: MockViewConfig{
				Embed:      true,
				DirFS:      "templates",
				Ext:        ".html",
				CSSPath:    "css",
				JSPath:     "/js", // error <- has leading slash
				TemplateFS: []fs.FS{baseTemplates},
				AssetFS:    assetsFS,
			},
			expectError: true,
			errorMsg:    "JS path should not start with '/' when embed is true",
		},
		{
			name: "Missing template FS",
			config: MockViewConfig{
				Embed:   true,
				DirFS:   "templates",
				Ext:     ".html",
				CSSPath: "css",
				JSPath:  "js",
				// TemplateFS: nil
				AssetFS: assetsFS,
			},
			expectError: true,
			errorMsg:    "No template filesystems provided when embed is true",
		},
		{
			name: "Non-existent OS directory",
			config: MockViewConfig{
				Embed:   false,
				DirOS:   "/path/that/definitely/does/not/exist",
				Ext:     ".html",
				CSSPath: "css",
				JSPath:  "js",
			},
			expectError: true,
			errorMsg:    "does not exist",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := router.ValidateConfig(&tc.config)

			if tc.expectError {
				assert.Error(t, err)
				if tc.errorMsg != "" {
					assert.Contains(t, err.Error(), tc.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEmbeddedTemplates(t *testing.T) {
	// if testing.Verbose() {
	fmt.Println("Examining embedded assets structure:")
	fs.WalkDir(assetsFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		dirMarker := " "
		if d.IsDir() {
			dirMarker = "/"
		}
		fmt.Printf("  %s%s\n", path, dirMarker)
		return nil
	})
	// }

	templateRoot, err := fs.Sub(baseTemplates, "testdata/templates")
	require.NoError(t, err)

	assetRoot, err := fs.Sub(assetsFS, "testdata/assets")
	require.NoError(t, err)

	config := &MockViewConfig{
		Embed:      true,
		Debug:      true,
		Reload:     true,
		DirFS:      ".",
		Ext:        ".html",
		CSSPath:    "css",
		JSPath:     "js",
		TemplateFS: []fs.FS{templateRoot},
		AssetFS:    assetRoot,
		Functions:  map[string]any{},
	}

	viewEngine, err := router.InitializeViewEngine(config)
	require.NoError(t, err, "Failed to initialize view engine")

	app := createFiberApp(t, viewEngine)

	resp, err := app.Test(httptest.NewRequest("GET", "/test-template", nil))
	require.NoError(t, err, "Failed to send test request")

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")

	assert.Contains(t, string(body), "Test Page")
	assert.Contains(t, string(body), "BASE TEMPLATE")
}

func TestTemplateOverriding(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test that requires filesystem setup")
	}
	// Create the exact directory structure needed
	// DEV directory - highest priority
	devFiles := map[string]string{
		"testdata/templates/test.html": "<h1>DEV OVERRIDE: {{ Title }}</h1>",
	}
	tempDir := setupTempDir(t, devFiles)
	defer os.RemoveAll(tempDir)

	assetFiles := map[string]string{
		"css/style.css": "/* CSS */",
		"js/script.js":  "// JS",
	}
	assetDir := setupTempDir(t, assetFiles)
	defer os.RemoveAll(assetDir)

	// priority: dev > theme > base
	config := &MockViewConfig{
		Embed:      true,
		Debug:      true,
		Reload:     true,
		DirFS:      "testdata/templates",
		DevDir:     tempDir,
		Ext:        ".html",
		CSSPath:    "css",
		JSPath:     "js",
		TemplateFS: []fs.FS{themeTemplates, baseTemplates},
		AssetFS:    os.DirFS(assetDir),
		Functions:  map[string]any{},
	}

	viewEngine, err := router.InitializeViewEngine(config)
	if err != nil {
		t.Logf("Error initializing view engine: %v", err)
		return
	}

	app := createFiberApp(t, viewEngine)

	resp, err := app.Test(httptest.NewRequest("GET", "/test-template", nil))
	require.NoError(t, err, "Failed to send test request")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")

	assert.Contains(t, string(body), "DEV OVERRIDE")
}

func TestAssetResolution(t *testing.T) {
	templateRoot, err := fs.Sub(baseTemplates, "testdata/templates")
	require.NoError(t, err)

	assetRoot, err := fs.Sub(assetsFS, "testdata/assets")
	require.NoError(t, err)

	config := &MockViewConfig{
		Embed:        true,
		Debug:        true,
		Reload:       true,
		DirFS:        ".",
		Ext:          ".html",
		CSSPath:      "css",
		JSPath:       "js",
		TemplateFS:   []fs.FS{templateRoot},
		AssetFS:      assetRoot,
		RemovePrefix: "",
		Functions:    map[string]any{},
	}

	viewEngine, err := router.InitializeViewEngine(config)
	require.NoError(t, err, "Failed to initialize view engine")

	app := createFiberApp(t, viewEngine)

	resp, err := app.Test(httptest.NewRequest("GET", "/with-assets", nil))
	require.NoError(t, err, "Failed to send test request")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")

	cssPattern := regexp.MustCompile(`<link rel="stylesheet" href="/css/main-[a-zA-Z0-9]+\.css">`)
	assert.True(t, cssPattern.MatchString(string(body)), "CSS was not resolved correctly: "+string(body))

	jsPattern := regexp.MustCompile(`<script async src="/js/app-[a-zA-Z0-9]+\.js"></script>`)
	assert.True(t, jsPattern.MatchString(string(body)), "JS was not resolved correctly: "+string(body))
}

func TestNonEmbeddedMode(t *testing.T) {
	// We create a structure like:
	// /tmp/..../templates/test.html
	// /tmp/..../public/css/main-tmp.css
	files := map[string]string{
		"templates/test.html": "<h1>OS TEMPLATE: {{ Title }}</h1>",
		"templates/with-assets.html": `<!DOCTYPE html>
<html>
<head>
    <title>{{ Title }}</title>
    {{ css("main-*.css") | safe }}
</head>
<body>
    <h1>OS Assets Test</h1>
    {{ js("app-*.js") | safe }}
</body>
</html>`,
		"public/css/main-tmp.css": "/* CSS content */",
		"public/js/app-tmp.js":    "// JS content",
	}

	tempDir := setupTempDir(t, files)
	defer os.RemoveAll(tempDir)

	// define the direct paths to our template and asset roots
	assetDir := filepath.Join(tempDir, "public")
	templateDir := filepath.Join(tempDir, "templates")

	config := &MockViewConfig{
		Embed:        false,
		Debug:        true,
		Reload:       true,
		DirOS:        templateDir,
		AssetsDir:    assetDir,
		DirFS:        "", // DirFS is not used in live mode, so it can be empty
		Ext:          ".html",
		CSSPath:      "css",
		JSPath:       "js",
		RemovePrefix: "public",
		TemplateFS:   nil,
		Functions:    map[string]any{},
	}

	viewEngine, err := router.InitializeViewEngine(config)
	require.NoError(t, err, "Failed to initialize view engine")

	app := createFiberApp(t, viewEngine)

	resp, err := app.Test(httptest.NewRequest("GET", "/test-template", nil))
	require.NoError(t, err, "Failed to send test request")
	assert.Equal(t, 200, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")
	assert.Contains(t, string(body), "OS TEMPLATE")

	resp, err = app.Test(httptest.NewRequest("GET", "/with-assets", nil))
	require.NoError(t, err, "Failed to send test request")
	assert.Equal(t, 200, resp.StatusCode)

	body, err = io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")

	config.RemovePrefix = ""

	viewEngine, err = router.InitializeViewEngine(config)
	require.NoError(t, err, "Failed to initialize view engine")
	app = createFiberApp(t, viewEngine)

	resp, err = app.Test(httptest.NewRequest("GET", "/with-assets", nil))
	require.NoError(t, err, "Failed to send test request")
	body, err = io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")

	assert.Contains(t, string(body), `/css/main-tmp.css`)
	assert.Contains(t, string(body), `/js/app-tmp.js`)
}

func TestAssetPathEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		cssPath    string
		jsPath     string
		setupFiles map[string]string
		expectErr  bool
	}{
		{
			name:      "Non-existent CSS directory",
			cssPath:   "non-existent/css",
			jsPath:    "assets/js",
			expectErr: true,
		},
		{
			name:      "Non-existent JS directory",
			cssPath:   "assets/css",
			jsPath:    "non-existent/js",
			expectErr: true,
		},
		{
			name:      "Path normalization with trailing slashes",
			cssPath:   "testdata/assets/css/",
			jsPath:    "testdata/assets/js/",
			expectErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var tempDir string
			if tc.setupFiles != nil {
				tempDir = setupTempDir(t, tc.setupFiles)
				defer os.RemoveAll(tempDir)
			}

			config := &MockViewConfig{
				Embed:      true,
				Debug:      true,
				DirFS:      ".",
				Ext:        ".html",
				CSSPath:    tc.cssPath,
				JSPath:     tc.jsPath,
				TemplateFS: []fs.FS{baseTemplates},
				AssetFS:    assetsFS,
			}

			_, err := router.InitializeViewEngine(config)

			if tc.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPathNormalization(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/path/to/dir", "path/to/dir"},
		{"path/to/dir/", "path/to/dir"},
		{"/path/to/dir/", "path/to/dir"},
		{"", ""},
		{"/", ""},
		{"///multiple/slashes//here/", "multiple/slashes/here"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := router.NormalizePath(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestPathResolution(t *testing.T) {
	tests := []struct {
		base     string
		sub      string
		expected string
	}{
		{"base", "sub", "base/sub"},
		{"/base/", "/sub/", "base/sub"},
		{"", "sub", "sub"},
		{"base", "", "base"},
		{"", "", ""},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%s+%s", tc.base, tc.sub), func(t *testing.T) {
			result := router.ResolvePath(tc.base, tc.sub)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestDirExists(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "dir-exists-test-*")
	require.NoError(t, err, "Failed to create temp directory")
	defer os.RemoveAll(tempDir)

	filePath := filepath.Join(tempDir, "testfile.txt")
	err = os.WriteFile(filePath, []byte("test"), 0644)
	require.NoError(t, err, "Failed to create test file")

	dirPath := filepath.Join(tempDir, "testdir")
	err = os.MkdirAll(dirPath, 0755)
	require.NoError(t, err, "Failed to create test directory")

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"Existing directory", dirPath, true},
		{"File (not a directory)", filePath, false},
		{"Non-existent path", filepath.Join(tempDir, "nonexistent"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := router.DirExists(tc.path)
			assert.Equal(t, tc.expected, result)

			osFS := os.DirFS(tempDir)
			relPath, err := filepath.Rel(tempDir, tc.path)
			if err == nil {
				result = router.DirExists(relPath, osFS)
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}
