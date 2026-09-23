package plugin

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
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
				Name    string `json:"Name"`
				Version string `json:"Version"`
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
