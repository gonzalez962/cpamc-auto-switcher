package plugin

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
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

func TestHandlePluginMethod_ManagementHandle_SingleFileSelfContainedHTML(t *testing.T) {
	reqJSON := []byte(`{"Method":"GET","Path":"/v0/resource/plugins/visual-profile/profiles"}`)
	raw, err := HandlePluginMethod("management.handle", reqJSON)
	if err != nil {
		t.Fatalf("unexpected error fetching HTML: %v", err)
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

	contentType := resp.Headers["Content-Type"]
	if len(contentType) == 0 || contentType[0] != "text/html; charset=utf-8" {
		t.Errorf("expected text/html Content-Type, got %v", contentType)
	}

	bodyBytes, err := base64.StdEncoding.DecodeString(resp.Body)
	if err != nil {
		t.Fatalf("failed to decode base64 body: %v", err)
	}
	bodyStr := string(bodyBytes)

	if len(bodyBytes) < 100000 {
		t.Errorf("expected single-file HTML to be >100KB, got %d bytes", len(bodyBytes))
	}

	// 1. Must contain inline <style> and <script>
	if !strings.Contains(bodyStr, "<style") {
		t.Errorf("expected inline <style> tag in single-file HTML")
	}
	if !strings.Contains(bodyStr, "<script") {
		t.Errorf("expected inline <script> tag in single-file HTML")
	}

	// 2. Must not contain external script src
	scriptSrcRegex := regexp.MustCompile(`(?i)<script\b[^>]*\bsrc\s*=`)
	if scriptSrcRegex.MatchString(bodyStr) {
		t.Errorf("expected no external script src in single-file HTML, found: %s", scriptSrcRegex.FindString(bodyStr))
	}

	// 3. Must not contain external stylesheet link href
	stylesheetRegex := regexp.MustCompile(`(?i)<link\b[^>]*\b(rel\s*=\s*["']?stylesheet|href\s*=\s*["'][^"']*\.css)`)
	if stylesheetRegex.MatchString(bodyStr) {
		t.Errorf("expected no external stylesheet link in single-file HTML, found: %s", stylesheetRegex.FindString(bodyStr))
	}

	// 4. Must not contain obsolete base tag
	if strings.Contains(bodyStr, "<base ") {
		t.Errorf("expected no obsolete <base> tag in single-file HTML")
	}

	// 5. Must not contain Vite modulePreload polyfill (removes dead fetch)
	if strings.Contains(strings.ToLower(bodyStr), "modulepreload") {
		t.Errorf("expected Vite modulePreload polyfill to be disabled, found modulepreload")
	}

	// 6. Must not contain external icon or link tags
	linkRegex := regexp.MustCompile(`(?i)<link\b[^>]*>`)
	if linkRegex.MatchString(bodyStr) {
		t.Errorf("expected zero <link> tags in single-file HTML, found: %s", linkRegex.FindString(bodyStr))
	}

	// 7. Must contain React root container
	if !strings.Contains(bodyStr, `<div id="root">`) {
		t.Errorf("expected root container div in HTML")
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

// TestHandlePluginMethod_ManagementHandle_HostSlashlessVsInternalIsolation explicitly tests and distinguishes:
// 1. Host-facing URL is strictly slashless "/profiles" (the only path registered in management.register and matched by host resourceRoutes).
// 2. In isolation, the internal plugin handler MatchProfilesRoute defensively tolerates trailing slash variants,
//    but runtime requests with trailing slashes are not routed by the host.
func TestHandlePluginMethod_ManagementHandle_HostSlashlessVsInternalIsolation(t *testing.T) {
	testRoutes := []struct {
		name string
		path string
	}{
		{"host registered slashless route (only supported host runtime path)", "/profiles"},
		{"internal isolation trailing slash fallback", "/profiles/"},
		{"internal isolation index.html fallback", "/profiles/index.html"},
		{"internal isolation standard plugin path slashless", "/v0/resource/plugins/visual-profile/profiles"},
		{"internal isolation standard plugin path slashful", "/v0/resource/plugins/visual-profile/profiles/"},
		{"internal isolation standard plugin path index.html", "/v0/resource/plugins/visual-profile/profiles/index.html"},
		{"internal isolation linux-amd64 binary path slashless", "/v0/resource/plugins/visual-profile-linux-amd64/profiles"},
		{"internal isolation linux-amd64 binary path slashful", "/v0/resource/plugins/visual-profile-linux-amd64/profiles/"},
		{"internal isolation arbitrary binary name slashless", "/v0/resource/plugins/custom-agent-profile-v2/profiles"},
		{"internal isolation arbitrary binary name slashful", "/v0/resource/plugins/custom-agent-profile-v2/profiles/"},
	}

	scriptSrcRegex := regexp.MustCompile(`(?i)<script\b[^>]*\bsrc\s*=`)
	stylesheetRegex := regexp.MustCompile(`(?i)<link\b[^>]*\b(rel\s*=\s*["']?stylesheet|href\s*=\s*["'][^"']*\.css)`)

	var expectedBody string
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
				t.Fatalf("expected status 200 for %q, got %d", tc.path, resp.StatusCode)
			}

			ct := resp.Headers["Content-Type"]
			if len(ct) == 0 || ct[0] != "text/html; charset=utf-8" {
				t.Errorf("expected text/html; charset=utf-8, got %v", ct)
			}

			bodyBytes, err := base64.StdEncoding.DecodeString(resp.Body)
			if err != nil {
				t.Fatalf("failed to decode base64 body: %v", err)
			}
			bodyStr := string(bodyBytes)

			if expectedBody == "" {
				expectedBody = bodyStr
			} else if bodyStr != expectedBody {
				t.Errorf("expected deterministic identical HTML for %q", tc.path)
			}

			if !strings.Contains(bodyStr, "<style") {
				t.Errorf("expected inline <style> tag in response for %q", tc.path)
			}
			if !strings.Contains(bodyStr, "<script") {
				t.Errorf("expected inline <script> tag in response for %q", tc.path)
			}
			if scriptSrcRegex.MatchString(bodyStr) {
				t.Errorf("expected no script src in response for %q", tc.path)
			}
			if stylesheetRegex.MatchString(bodyStr) {
				t.Errorf("expected no stylesheet link in response for %q", tc.path)
			}
			if strings.Contains(bodyStr, "<base ") {
				t.Errorf("expected no <base> tag in response for %q", tc.path)
			}
		})
	}
}

func TestHandlePluginMethod_ManagementHandle_SubresourceRejection(t *testing.T) {
	subresourcePaths := []string{
		"/profiles/assets/index.js",
		"/profiles/assets/index.css",
		"/v0/resource/plugins/visual-profile/profiles/assets/index.js",
		"/v0/resource/plugins/visual-profile/profiles/assets/style.css",
		"/v0/resource/plugins/visual-profile-linux-amd64/profiles/assets/app.js",
	}

	for _, p := range subresourcePaths {
		t.Run(p, func(t *testing.T) {
			reqJSON := []byte(fmt.Sprintf(`{"Method":"GET","Path":"%s"}`, p))
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

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("expected status 404 for separate subresource %q, got %d", p, resp.StatusCode)
			}
		})
	}
}

func TestWebAssetsFS_SingleFileEmbed(t *testing.T) {
	var fileCount int
	var foundIndexHtml bool
	var separateAssets []string

	err := fs.WalkDir(web.AssetsFS, "assets", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			fileCount++
			if p == "assets/index.html" {
				foundIndexHtml = true
			} else {
				separateAssets = append(separateAssets, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to inspect AssetsFS: %v", err)
	}

	if !foundIndexHtml {
		t.Fatalf("expected assets/index.html to be embedded in AssetsFS")
	}
	if len(separateAssets) > 0 {
		t.Errorf("expected no separate asset files in AssetsFS, found: %v", separateAssets)
	}
	if fileCount != 1 {
		t.Errorf("expected exactly 1 embedded asset file (index.html), got %d", fileCount)
	}
}
