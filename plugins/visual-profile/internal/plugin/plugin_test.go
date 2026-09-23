package plugin

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"visual-profile/internal/version"
	"visual-profile/internal/web"
)

func TestHandlePluginMethod_PluginRegister(t *testing.T) {
	methods := []string{"plugin.register", "plugin.reconfigure"}
	for _, m := range methods {
		raw, err := HandlePluginMethod(m, nil)
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", m, err)
		}

		var env Envelope
		if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
			t.Fatalf("expected OK envelope for %s, got: %s", m, string(raw))
		}

		var payload struct {
			SchemaVersion uint32 `json:"schema_version"`
			Metadata      struct {
				Name             string `json:"Name"`
				Version          string `json:"Version"`
				Author           string `json:"Author"`
				GitHubRepository string `json:"GitHubRepository"`
			} `json:"metadata"`
			Capabilities struct {
				ManagementAPI bool `json:"management_api"`
			} `json:"capabilities"`
		}
		if err := json.Unmarshal(env.Result, &payload); err != nil {
			t.Fatalf("failed to unmarshal registration payload: %v", err)
		}

		if payload.SchemaVersion != 6 {
			t.Errorf("expected schema version 6, got %d", payload.SchemaVersion)
		}
		if payload.Metadata.Name != "visual-profile" {
			t.Errorf("expected plugin name 'visual-profile', got %q", payload.Metadata.Name)
		}
		if payload.Metadata.Version != version.Version {
			t.Errorf("expected version %q, got %q", version.Version, payload.Metadata.Version)
		}
		if payload.Metadata.Author != "gonzalez962" {
			t.Errorf("expected author 'gonzalez962', got %q", payload.Metadata.Author)
		}
		if payload.Metadata.GitHubRepository != "https://github.com/gonzalez962/cpamc-auto-switcher" {
			t.Errorf("expected GitHubRepository 'https://github.com/gonzalez962/cpamc-auto-switcher', got %q", payload.Metadata.GitHubRepository)
		}
		if !payload.Capabilities.ManagementAPI {
			t.Errorf("expected management_api capability to be true")
		}
	}
}

func TestHandlePluginMethod_ManagementRegister(t *testing.T) {
	raw, err := HandlePluginMethod("management.register", nil)
	if err != nil {
		t.Fatalf("unexpected error for management.register: %v", err)
	}

	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
		t.Fatalf("expected OK envelope for management.register, got: %s", string(raw))
	}

	var payload struct {
		Resources []struct {
			Path string `json:"Path"`
			Menu string `json:"Menu"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(env.Result, &payload); err != nil {
		t.Fatalf("failed to unmarshal management register payload: %v", err)
	}

	if len(payload.Resources) == 0 {
		t.Fatal("expected at least one registered resource")
	}

	foundProfilesRoute := false
	for _, res := range payload.Resources {
		if res.Path == "/profiles" {
			foundProfilesRoute = true
			if res.Menu != "Visual Profile" {
				t.Errorf("expected Menu 'Visual Profile', got %q", res.Menu)
			}
		}
	}
	if !foundProfilesRoute {
		t.Errorf("expected route '/profiles' in registered resources, got %+v", payload.Resources)
	}
}

func TestMatchProfilesRoute_ExactSegmentScoped(t *testing.T) {
	validCases := []struct {
		path            string
		expectedSubpath string
	}{
		{"/profiles", "index.html"},
		{"/profiles/", "index.html"},
		{"/profiles/index.html", "index.html"},
		{"/profiles/assets/index.js", "assets/index.js"},
		{"/v0/resource/plugins/visual-profile/profiles", "index.html"},
		{"/v0/resource/plugins/visual-profile/profiles/", "index.html"},
		{"/v0/resource/plugins/visual-profile/profiles/index.html", "index.html"},
		{"/v0/resource/plugins/visual-profile/profiles/assets/index.js", "assets/index.js"},
	}

	for _, tc := range validCases {
		subpath, matched := MatchProfilesRoute(tc.path)
		if !matched {
			t.Errorf("expected MatchProfilesRoute(%q) to match, got matched=false", tc.path)
		}
		if subpath != tc.expectedSubpath {
			t.Errorf("MatchProfilesRoute(%q) subpath = %q, expected %q", tc.path, subpath, tc.expectedSubpath)
		}
	}

	invalidCases := []string{
		"/profiles_extra",
		"/profiles-attack/secret",
		"/profiles_test/index.html",
		"/v0/resource/plugins/visual-profile/profiles_extra",
		"/v0/resource/plugins/visual-profile/notprofiles",
		"/v0/resource/plugins/visual-profile/myprofiles",
		"/myprofiles",
		"/other/endpoint",
		"",
	}

	for _, path := range invalidCases {
		_, matched := MatchProfilesRoute(path)
		if matched {
			t.Errorf("expected MatchProfilesRoute(%q) to reject, got matched=true", path)
		}
	}
}

func TestHandlePluginMethod_ManagementHandle_Index(t *testing.T) {
	reqJSON := []byte(`{"Method":"GET","Path":"/v0/resource/plugins/visual-profile/profiles"}`)
	raw, err := HandlePluginMethod("management.handle", reqJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
		t.Fatalf("expected OK envelope, got: %s", string(raw))
	}

	var resp ManagementResponsePayload
	if err := json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatalf("failed to decode management response: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("expected status code 200, got %d", resp.StatusCode)
	}

	bodyBytes, err := base64.StdEncoding.DecodeString(resp.Body)
	if err != nil {
		t.Fatalf("failed to decode base64 body: %v", err)
	}
	if len(bodyBytes) == 0 {
		t.Fatal("expected non-empty body")
	}

	contentType := resp.Headers["Content-Type"]
	if len(contentType) == 0 || contentType[0] != "text/html; charset=utf-8" {
		t.Errorf("expected text/html Content-Type, got %v", contentType)
	}
}

func TestHandlePluginMethod_ManagementHandle_ExactRouteScope(t *testing.T) {
	hostileRoutes := []string{
		"/v0/resource/plugins/visual-profile/profiles_test",
		"/v0/resource/plugins/visual-profile/profiles-attack/secret",
		"/v0/resource/plugins/visual-profile/myprofiles",
		"/other/route",
	}

	for _, route := range hostileRoutes {
		reqJSON := []byte(fmt.Sprintf(`{"Method":"GET","Path":"%s"}`, route))
		raw, err := HandlePluginMethod("management.handle", reqJSON)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var env Envelope
		if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
			t.Fatalf("expected OK envelope wrapping response, got: %s", string(raw))
		}

		var resp ManagementResponsePayload
		if err := json.Unmarshal(env.Result, &resp); err != nil {
			t.Fatalf("failed to decode management response: %v", err)
		}

		if resp.StatusCode != 404 {
			t.Errorf("expected status code 404 for non-matching route %q, got %d", route, resp.StatusCode)
		}
	}
}

func TestHandlePluginMethod_ManagementHandle_MethodRestrictions(t *testing.T) {
	disallowedMethods := []string{"POST", "PUT", "DELETE", "PATCH"}
	for _, m := range disallowedMethods {
		reqJSON := []byte(fmt.Sprintf(`{"Method":"%s","Path":"/v0/resource/plugins/visual-profile/profiles"}`, m))
		raw, err := HandlePluginMethod("management.handle", reqJSON)
		if err != nil {
			t.Fatalf("unexpected error for method %s: %v", m, err)
		}

		var env Envelope
		if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
			t.Fatalf("expected OK envelope wrapping 405 response, got: %s", string(raw))
		}

		var resp ManagementResponsePayload
		if err := json.Unmarshal(env.Result, &resp); err != nil {
			t.Fatalf("failed to decode management response: %v", err)
		}

		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 Method Not Allowed for %s, got %d", m, resp.StatusCode)
		}
	}

	// HEAD method should return 200 OK with empty body
	headReqJSON := []byte(`{"Method":"HEAD","Path":"/v0/resource/plugins/visual-profile/profiles"}`)
	rawHead, err := HandlePluginMethod("management.handle", headReqJSON)
	if err != nil {
		t.Fatalf("unexpected error for HEAD: %v", err)
	}

	var envHead Envelope
	if err := json.Unmarshal(rawHead, &envHead); err != nil || !envHead.OK {
		t.Fatalf("expected OK envelope wrapping HEAD response, got: %s", string(rawHead))
	}

	var respHead ManagementResponsePayload
	if err := json.Unmarshal(envHead.Result, &respHead); err != nil {
		t.Fatalf("failed to decode HEAD response: %v", err)
	}

	if respHead.StatusCode != 200 {
		t.Errorf("expected status 200 for HEAD, got %d", respHead.StatusCode)
	}
	if respHead.Body != "" {
		t.Errorf("expected empty body for HEAD, got %q", respHead.Body)
	}
	if len(respHead.Headers["Content-Type"]) == 0 {
		t.Errorf("expected Content-Type header on HEAD response")
	}
}

func TestHandlePluginMethod_ManagementHandle_HostilePathsAndTraversal(t *testing.T) {
	hostilePaths := []string{
		"/v0/resource/plugins/visual-profile/profiles/../secret.txt",
		"/v0/resource/plugins/visual-profile/profiles/../../etc/passwd",
		"/v0/resource/plugins/visual-profile/profiles/%2e%2e%2fsecret",
		"/v0/resource/plugins/visual-profile/profiles/assets/..%2f..%2fsecret",
		"/v0/resource/plugins/visual-profile/profiles/..\\..\\windows\\win.ini",
		"/v0/resource/plugins/visual-profile/profiles/index.html\x00.evil",
	}

	for _, hp := range hostilePaths {
		reqJSON := []byte(fmt.Sprintf(`{"Method":"GET","Path":"%s"}`, hp))
		raw, err := HandlePluginMethod("management.handle", reqJSON)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var env Envelope
		if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
			t.Fatalf("expected OK envelope wrapping error response, got: %s", string(raw))
		}

		var resp ManagementResponsePayload
		if err := json.Unmarshal(env.Result, &resp); err != nil {
			t.Fatalf("failed to decode management response: %v", err)
		}

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status code 400 Bad Request for hostile path %q, got %d", hp, resp.StatusCode)
		}
	}
}

func TestHandlePluginMethod_ManagementHandle_ValidEmbeddedJSRoute(t *testing.T) {
	// Dynamically discover embedded JS file generated by Vite
	var jsRelPath string
	err := fs.WalkDir(web.AssetsFS, "assets", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr == nil && !d.IsDir() && strings.HasSuffix(p, ".js") {
			jsRelPath = strings.TrimPrefix(p, "assets/")
			return fs.SkipAll
		}
		return nil
	})
	if err != nil || jsRelPath == "" {
		t.Fatalf("failed to locate embedded JS file in AssetsFS: %v", err)
	}

	reqJSON := []byte(fmt.Sprintf(`{"Method":"GET","Path":"/v0/resource/plugins/visual-profile/profiles/%s"}`, jsRelPath))
	raw, err := HandlePluginMethod("management.handle", reqJSON)
	if err != nil {
		t.Fatalf("unexpected error fetching valid embedded JS: %v", err)
	}

	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
		t.Fatalf("expected OK envelope, got: %s", string(raw))
	}

	var resp ManagementResponsePayload
	if err := json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatalf("failed to decode management response: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("expected status code 200 for embedded JS %q, got %d", jsRelPath, resp.StatusCode)
	}

	contentType := resp.Headers["Content-Type"]
	if len(contentType) == 0 || !strings.Contains(contentType[0], "javascript") {
		t.Errorf("expected javascript content-type for %q, got %v", jsRelPath, contentType)
	}

	bodyBytes, err := base64.StdEncoding.DecodeString(resp.Body)
	if err != nil {
		t.Fatalf("failed to decode base64 JS body: %v", err)
	}
	if len(bodyBytes) == 0 {
		t.Fatal("expected non-empty JS body")
	}
}

func TestHandlePluginMethod_ManagementHandle_NotFound(t *testing.T) {
	reqJSON := []byte(`{"Method":"GET","Path":"/v0/resource/plugins/visual-profile/profiles/nonexistent.xyz"}`)
	raw, err := HandlePluginMethod("management.handle", reqJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
		t.Fatalf("expected OK envelope wrapping 404 response, got: %s", string(raw))
	}

	var resp ManagementResponsePayload
	if err := json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatalf("failed to decode management response: %v", err)
	}

	if resp.StatusCode != 404 {
		t.Errorf("expected status code 404 for missing asset, got %d", resp.StatusCode)
	}
}

func TestHandlePluginMethod_ManagementHandle_MalformedJSON(t *testing.T) {
	reqJSON := []byte(`{"Method": "GET", invalid-json`)
	raw, err := HandlePluginMethod("management.handle", reqJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
		t.Fatalf("expected OK envelope wrapping 400 response, got: %s", string(raw))
	}

	var resp ManagementResponsePayload
	if err := json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatalf("failed to decode management response: %v", err)
	}

	if resp.StatusCode != 400 {
		t.Errorf("expected status code 400 for malformed json, got %d", resp.StatusCode)
	}
}

func TestHandlePluginMethod_UnknownMethod(t *testing.T) {
	raw, err := HandlePluginMethod("unknown.foo.bar", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if env.OK {
		t.Errorf("expected OK=false for unknown method")
	}
	if env.Error == nil || env.Error.Code != "unknown_method" {
		t.Errorf("expected unknown_method error code, got %+v", env.Error)
	}
}

func TestDeriveProfilesBasePath(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{"root profiles without slash", "/profiles", "/profiles/"},
		{"root profiles with slash", "/profiles/", "/profiles/"},
		{"root profiles with index.html", "/profiles/index.html", "/profiles/"},
		{"root profiles relative without slash", "profiles", "/profiles/"},
		{"root profiles relative with slash", "profiles/", "/profiles/"},
		{"standard plugin path without slash", "/v0/resource/plugins/visual-profile/profiles", "/v0/resource/plugins/visual-profile/profiles/"},
		{"standard plugin path with slash", "/v0/resource/plugins/visual-profile/profiles/", "/v0/resource/plugins/visual-profile/profiles/"},
		{"linux-amd64 binary path without slash", "/v0/resource/plugins/visual-profile-linux-amd64/profiles", "/v0/resource/plugins/visual-profile-linux-amd64/profiles/"},
		{"linux-amd64 binary path with slash", "/v0/resource/plugins/visual-profile-linux-amd64/profiles/", "/v0/resource/plugins/visual-profile-linux-amd64/profiles/"},
		{"linux-amd64 binary path with index.html", "/v0/resource/plugins/visual-profile-linux-amd64/profiles/index.html", "/v0/resource/plugins/visual-profile-linux-amd64/profiles/"},
		{"arbitrary plugin binary name", "/v0/resource/plugins/custom-agent-profile-v2/profiles", "/v0/resource/plugins/custom-agent-profile-v2/profiles/"},
		{"arbitrary plugin binary name with slash", "/v0/resource/plugins/custom-agent-profile-v2/profiles/", "/v0/resource/plugins/custom-agent-profile-v2/profiles/"},
		{"path with query string and hash", "/v0/resource/plugins/visual-profile/profiles?tab=graph#settings", "/v0/resource/plugins/visual-profile/profiles/"},
		{"path with url-encoded chars", "/v0/resource/plugins/%76isual-profile/profiles", "/v0/resource/plugins/visual-profile/profiles/"},
		{"empty path fallback", "", "/profiles/"},
		{"non-matching path fallback", "/v0/management/other", "/profiles/"},
		{"protocol relative injection rejected", "//evil.com/profiles", "/profiles/"},
		{"http scheme injection rejected", "http://evil.com/profiles", "/profiles/"},
		{"https scheme injection rejected", "https://evil.com/profiles", "/profiles/"},
		{"markup tag injection rejected", "/foo<script>/profiles", "/profiles/"},
		{"quote attribute breakout rejected", "/foo\"bar/profiles", "/profiles/"},
		{"single quote attribute breakout rejected", "/foo'bar/profiles", "/profiles/"},
		{"ampersand injection rejected", "/foo&bar/profiles", "/profiles/"},
		{"directory traversal rejected", "/v0/resource/plugins/../../profiles", "/profiles/"},
		{"whitespace injection rejected", "/v0/resource/plugins/foo bar/profiles", "/profiles/"},
		{"newline injection rejected", "/v0/resource/plugins/foo\nbar/profiles", "/profiles/"},
		{"null byte rejected", "/v0/resource/plugins/foo\x00bar/profiles", "/profiles/"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := DeriveProfilesBasePath(tc.input)
			if got != tc.expected {
				t.Errorf("DeriveProfilesBasePath(%q) = %q, expected %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestInjectBaseTag(t *testing.T) {
	t.Run("injects into head before script and link", func(t *testing.T) {
		html := []byte(`<!DOCTYPE html><html><head><meta charset="UTF-8"><script src="./assets/app.js"></script><link rel="stylesheet" href="./assets/style.css"></head><body></body></html>`)
		base := "/v0/resource/plugins/visual-profile-linux-amd64/profiles/"
		result := string(InjectBaseTag(html, base))

		expectedTag := `<base href="/v0/resource/plugins/visual-profile-linux-amd64/profiles/">`
		if !strings.Contains(result, expectedTag) {
			t.Fatalf("expected injected base tag %q in %s", expectedTag, result)
		}

		basePos := strings.Index(result, "<base ")
		scriptPos := strings.Index(result, "<script ")
		linkPos := strings.Index(result, "<link ")

		if basePos == -1 || scriptPos == -1 || linkPos == -1 {
			t.Fatalf("missing expected tags in result: %s", result)
		}
		if basePos > scriptPos {
			t.Errorf("expected <base> before <script>, got basePos=%d, scriptPos=%d", basePos, scriptPos)
		}
		if basePos > linkPos {
			t.Errorf("expected <base> before <link>, got basePos=%d, linkPos=%d", basePos, linkPos)
		}
	})

	t.Run("replaces existing base tag", func(t *testing.T) {
		html := []byte(`<html><head><base href="/old/path/"><title>Test</title></head></html>`)
		base := "/v0/resource/plugins/new-plugin/profiles/"
		result := string(InjectBaseTag(html, base))

		if strings.Contains(result, "/old/path/") {
			t.Errorf("expected old base path to be replaced, got: %s", result)
		}
		expectedTag := `<base href="/v0/resource/plugins/new-plugin/profiles/">`
		if !strings.Contains(result, expectedTag) {
			t.Errorf("expected new base tag %q, got: %s", expectedTag, result)
		}
		if strings.Count(result, "<base ") != 1 {
			t.Errorf("expected exactly 1 base tag, got %d in %s", strings.Count(result, "<base "), result)
		}
	})

	t.Run("escapes special characters in base href", func(t *testing.T) {
		html := []byte(`<html><head></head></html>`)
		base := `/profiles/"><script>`
		result := string(InjectBaseTag(html, base))

		if strings.Contains(result, `<script>`) {
			t.Errorf("expected base href to be HTML-escaped, got: %s", result)
		}
		if !strings.Contains(result, `&#34;&gt;&lt;script&gt;`) {
			t.Errorf("expected escaped quote and brackets, got: %s", result)
		}
	})

	t.Run("fallback prepend when no head tag", func(t *testing.T) {
		html := []byte(`<div>no head</div>`)
		base := "/profiles/"
		result := string(InjectBaseTag(html, base))

		if !strings.HasPrefix(result, `<base href="/profiles/">`) {
			t.Errorf("expected prepended base tag, got: %s", result)
		}
	})
}

func TestHandlePluginMethod_ManagementHandle_BaseTagInjection(t *testing.T) {
	testRoutes := []struct {
		name         string
		path         string
		expectedBase string
	}{
		{"root profiles no slash", "/profiles", "/profiles/"},
		{"root profiles with slash", "/profiles/", "/profiles/"},
		{"root profiles index.html", "/profiles/index.html", "/profiles/"},
		{"visual-profile no slash", "/v0/resource/plugins/visual-profile/profiles", "/v0/resource/plugins/visual-profile/profiles/"},
		{"visual-profile with slash", "/v0/resource/plugins/visual-profile/profiles/", "/v0/resource/plugins/visual-profile/profiles/"},
		{"linux-amd64 binary no slash", "/v0/resource/plugins/visual-profile-linux-amd64/profiles", "/v0/resource/plugins/visual-profile-linux-amd64/profiles/"},
		{"linux-amd64 binary with slash", "/v0/resource/plugins/visual-profile-linux-amd64/profiles/", "/v0/resource/plugins/visual-profile-linux-amd64/profiles/"},
		{"arbitrary plugin name", "/v0/resource/plugins/custom-profile-builder/profiles", "/v0/resource/plugins/custom-profile-builder/profiles/"},
	}

	for _, tc := range testRoutes {
		t.Run(tc.name, func(t *testing.T) {
			reqJSON := []byte(fmt.Sprintf(`{"Method":"GET","Path":"%s"}`, tc.path))
			raw, err := HandlePluginMethod("management.handle", reqJSON)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var env Envelope
			if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
				t.Fatalf("expected OK envelope, got: %s", string(raw))
			}

			var resp ManagementResponsePayload
			if err := json.Unmarshal(env.Result, &resp); err != nil {
				t.Fatalf("failed to decode management response: %v", err)
			}

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected status 200, got %d", resp.StatusCode)
			}

			bodyBytes, err := base64.StdEncoding.DecodeString(resp.Body)
			if err != nil {
				t.Fatalf("failed to decode base64 body: %v", err)
			}
			bodyStr := string(bodyBytes)

			expectedBaseTag := fmt.Sprintf(`<base href="%s">`, tc.expectedBase)
			if !strings.Contains(bodyStr, expectedBaseTag) {
				t.Errorf("expected body to contain %q, but got:\n%s", expectedBaseTag, bodyStr)
			}

			baseIdx := strings.Index(bodyStr, "<base ")
			scriptIdx := strings.Index(bodyStr, "<script ")
			linkIdx := strings.Index(bodyStr, "<link ")

			if baseIdx == -1 {
				t.Errorf("base tag not found in response HTML")
			}
			if scriptIdx != -1 && baseIdx > scriptIdx {
				t.Errorf("expected <base> before <script>, got baseIdx=%d, scriptIdx=%d", baseIdx, scriptIdx)
			}
			if linkIdx != -1 && baseIdx > linkIdx {
				t.Errorf("expected <base> before <link>, got baseIdx=%d, linkIdx=%d", baseIdx, linkIdx)
			}
		})
	}
}

func TestHandlePluginMethod_ManagementHandle_CSSServing(t *testing.T) {
	var cssRelPath string
	err := fs.WalkDir(web.AssetsFS, "assets", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr == nil && !d.IsDir() && strings.HasSuffix(p, ".css") {
			cssRelPath = strings.TrimPrefix(p, "assets/")
			return fs.SkipAll
		}
		return nil
	})
	if err != nil || cssRelPath == "" {
		t.Fatalf("failed to locate embedded CSS file in AssetsFS: %v", err)
	}

	testPaths := []string{
		fmt.Sprintf("/v0/resource/plugins/visual-profile-linux-amd64/profiles/%s", cssRelPath),
		fmt.Sprintf("/profiles/%s", cssRelPath),
	}

	for _, p := range testPaths {
		t.Run(p, func(t *testing.T) {
			reqJSON := []byte(fmt.Sprintf(`{"Method":"GET","Path":"%s"}`, p))
			raw, err := HandlePluginMethod("management.handle", reqJSON)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var env Envelope
			if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
				t.Fatalf("expected OK envelope, got: %s", string(raw))
			}

			var resp ManagementResponsePayload
			if err := json.Unmarshal(env.Result, &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected status code 200, got %d", resp.StatusCode)
			}

			ct := resp.Headers["Content-Type"]
			if len(ct) == 0 || !strings.Contains(ct[0], "text/css") {
				t.Errorf("expected text/css content type, got %v", ct)
			}

			bodyBytes, err := base64.StdEncoding.DecodeString(resp.Body)
			if err != nil {
				t.Fatalf("failed to decode base64: %v", err)
			}
			if len(bodyBytes) == 0 {
				t.Fatal("expected non-empty CSS body")
			}
			if strings.Contains(string(bodyBytes), "<base ") {
				t.Errorf("CSS response should not contain <base> tag")
			}
		})
	}
}

func TestHandlePluginMethod_ManagementHandle_URLResolutionRegression(t *testing.T) {
	cases := []struct {
		name     string
		menuPath string
	}{
		{"linux-amd64 without trailing slash", "/v0/resource/plugins/visual-profile-linux-amd64/profiles"},
		{"linux-amd64 with trailing slash", "/v0/resource/plugins/visual-profile-linux-amd64/profiles/"},
		{"root profiles without trailing slash", "/profiles"},
		{"root profiles with trailing slash", "/profiles/"},
		{"arbitrary plugin binary name", "/v0/resource/plugins/custom-binary-amd64/profiles"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reqJSON := []byte(fmt.Sprintf(`{"Method":"GET","Path":"%s"}`, tc.menuPath))
			raw, err := HandlePluginMethod("management.handle", reqJSON)
			if err != nil {
				t.Fatalf("unexpected error fetching HTML: %v", err)
			}
			var env Envelope
			if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
				t.Fatalf("expected OK envelope: %s", string(raw))
			}
			var resp ManagementResponsePayload
			_ = json.Unmarshal(env.Result, &resp)
			bodyBytes, _ := base64.StdEncoding.DecodeString(resp.Body)
			html := string(bodyBytes)

			baseRegex := regexp.MustCompile(`<base\s+href="([^"]+)"`)
			baseMatches := baseRegex.FindStringSubmatch(html)
			if len(baseMatches) < 2 {
				t.Fatalf("expected <base href=\"...\"> in HTML, got:\n%s", html)
			}
			baseHrefStr := baseMatches[1]

			baseURL, err := url.Parse(baseHrefStr)
			if err != nil {
				t.Fatalf("failed to parse base href %q: %v", baseHrefStr, err)
			}

			linkRegex := regexp.MustCompile(`<link\b[^>]*\bhref="([^"]+)"`)
			linkMatches := linkRegex.FindAllStringSubmatch(html, -1)
			if len(linkMatches) == 0 {
				t.Fatalf("expected at least one <link href=...> in HTML")
			}

			for _, match := range linkMatches {
				relHref := match[1]
				relURL, err := url.Parse(relHref)
				if err != nil {
					t.Fatalf("failed to parse rel href %q: %v", relHref, err)
				}

				resolvedURL := baseURL.ResolveReference(relURL)
				resolvedPath := resolvedURL.Path

				if !strings.Contains(resolvedPath, "/profiles/assets/") {
					t.Errorf("resolved path %q does not contain '/profiles/assets/'; this causes 404 on host!", resolvedPath)
				}

				assetReqJSON := []byte(fmt.Sprintf(`{"Method":"GET","Path":"%s"}`, resolvedPath))
				rawAsset, err := HandlePluginMethod("management.handle", assetReqJSON)
				if err != nil {
					t.Fatalf("failed to fetch resolved CSS asset %q: %v", resolvedPath, err)
				}
				var envAsset Envelope
				_ = json.Unmarshal(rawAsset, &envAsset)
				var respAsset ManagementResponsePayload
				_ = json.Unmarshal(envAsset.Result, &respAsset)

				if respAsset.StatusCode != http.StatusOK {
					t.Errorf("expected 200 OK for resolved asset %q, got %d", resolvedPath, respAsset.StatusCode)
				}
			}

			scriptRegex := regexp.MustCompile(`<script\b[^>]*\bsrc="([^"]+)"`)
			scriptMatches := scriptRegex.FindAllStringSubmatch(html, -1)
			if len(scriptMatches) == 0 {
				t.Fatalf("expected at least one <script src=...> in HTML")
			}

			for _, match := range scriptMatches {
				relSrc := match[1]
				relURL, err := url.Parse(relSrc)
				if err != nil {
					t.Fatalf("failed to parse rel src %q: %v", relSrc, err)
				}

				resolvedURL := baseURL.ResolveReference(relURL)
				resolvedPath := resolvedURL.Path

				if !strings.Contains(resolvedPath, "/profiles/assets/") {
					t.Errorf("resolved JS path %q does not contain '/profiles/assets/'", resolvedPath)
				}

				assetReqJSON := []byte(fmt.Sprintf(`{"Method":"GET","Path":"%s"}`, resolvedPath))
				rawAsset, err := HandlePluginMethod("management.handle", assetReqJSON)
				if err != nil {
					t.Fatalf("failed to fetch resolved JS asset %q: %v", resolvedPath, err)
				}
				var envAsset Envelope
				_ = json.Unmarshal(rawAsset, &envAsset)
				var respAsset ManagementResponsePayload
				_ = json.Unmarshal(envAsset.Result, &respAsset)

				if respAsset.StatusCode != http.StatusOK {
					t.Errorf("expected 200 OK for resolved JS asset %q, got %d", resolvedPath, respAsset.StatusCode)
				}
			}
		})
	}
}
