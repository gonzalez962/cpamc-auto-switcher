package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"visual-profile/internal/plugin"
	"visual-profile/internal/version"
	"visual-profile/internal/web"
)

func TestPluginMetadataAndRegistration(t *testing.T) {
	raw, err := plugin.HandlePluginMethod("plugin.register", nil)
	if err != nil {
		t.Fatalf("failed to call plugin.register: %v", err)
	}

	var env plugin.Envelope
	if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
		t.Fatalf("expected OK envelope, got: %s", string(raw))
	}

	var payload struct {
		SchemaVersion uint32 `json:"schema_version"`
		Metadata      struct {
			Name    string `json:"Name"`
			Version string `json:"Version"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(env.Result, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if payload.SchemaVersion != 6 {
		t.Errorf("expected schema version 6, got %d", payload.SchemaVersion)
	}
	if payload.Metadata.Name != "visual-profile" {
		t.Errorf("expected plugin name 'visual-profile', got %q", payload.Metadata.Name)
	}
	if payload.Metadata.Version != version.Version {
		t.Errorf("expected plugin version %q, got %q", version.Version, payload.Metadata.Version)
	}
}

func TestManagementResourceAndHandling(t *testing.T) {
	// 1. management.register returns /profiles
	rawReg, err := plugin.HandlePluginMethod("management.register", nil)
	if err != nil {
		t.Fatalf("failed to call management.register: %v", err)
	}
	var envReg plugin.Envelope
	if err := json.Unmarshal(rawReg, &envReg); err != nil || !envReg.OK {
		t.Fatalf("expected OK envelope for management.register, got: %s", string(rawReg))
	}

	// 2. management.handle serves index.html
	reqJSON := []byte(`{"Method":"GET","Path":"/v0/resource/plugins/visual-profile/profiles"}`)
	rawHandle, err := plugin.HandlePluginMethod("management.handle", reqJSON)
	if err != nil {
		t.Fatalf("failed to call management.handle: %v", err)
	}

	var envHandle plugin.Envelope
	if err := json.Unmarshal(rawHandle, &envHandle); err != nil || !envHandle.OK {
		t.Fatalf("expected OK envelope for management.handle, got: %s", string(rawHandle))
	}

	var respPayload plugin.ManagementResponsePayload
	if err := json.Unmarshal(envHandle.Result, &respPayload); err != nil {
		t.Fatalf("failed to unmarshal management response: %v", err)
	}

	if respPayload.StatusCode != 200 {
		t.Errorf("expected status code 200, got %d", respPayload.StatusCode)
	}

	// 3. management.handle rejects malformed JSON with 400 Bad Request
	malformedJSON := []byte(`{not valid json`)
	rawBad, err := plugin.HandlePluginMethod("management.handle", malformedJSON)
	if err != nil {
		t.Fatalf("failed to call management.handle with malformed JSON: %v", err)
	}
	var envBad plugin.Envelope
	if err := json.Unmarshal(rawBad, &envBad); err != nil || !envBad.OK {
		t.Fatalf("expected OK envelope wrapping 400 response, got: %s", string(rawBad))
	}
	var respBad plugin.ManagementResponsePayload
	if err := json.Unmarshal(envBad.Result, &respBad); err != nil {
		t.Fatalf("failed to unmarshal bad response: %v", err)
	}
	if respBad.StatusCode != 400 {
		t.Errorf("expected status code 400 for malformed json, got %d", respBad.StatusCode)
	}

	// 4. Exact segment-scoped routing: non-matching segment returns 404
	badRouteJSON := []byte(`{"Method":"GET","Path":"/v0/resource/plugins/visual-profile/profiles_extra"}`)
	rawBadRoute, err := plugin.HandlePluginMethod("management.handle", badRouteJSON)
	if err != nil {
		t.Fatalf("failed to call management.handle: %v", err)
	}
	var envBadRoute plugin.Envelope
	_ = json.Unmarshal(rawBadRoute, &envBadRoute)
	var respBadRoute plugin.ManagementResponsePayload
	_ = json.Unmarshal(envBadRoute.Result, &respBadRoute)
	if respBadRoute.StatusCode != 404 {
		t.Errorf("expected status code 404 for un-scoped route, got %d", respBadRoute.StatusCode)
	}

	// 5. Method restrictions: POST returns 405 Method Not Allowed
	postJSON := []byte(`{"Method":"POST","Path":"/v0/resource/plugins/visual-profile/profiles"}`)
	rawPost, err := plugin.HandlePluginMethod("management.handle", postJSON)
	if err != nil {
		t.Fatalf("failed to call management.handle: %v", err)
	}
	var envPost plugin.Envelope
	_ = json.Unmarshal(rawPost, &envPost)
	var respPost plugin.ManagementResponsePayload
	_ = json.Unmarshal(envPost.Result, &respPost)
	if respPost.StatusCode != 405 {
		t.Errorf("expected status code 405 for POST, got %d", respPost.StatusCode)
	}

	// 6. Hostile path traversal returns 400 Bad Request
	hostileJSON := []byte(`{"Method":"GET","Path":"/v0/resource/plugins/visual-profile/profiles/../../secret.txt"}`)
	rawHostile, err := plugin.HandlePluginMethod("management.handle", hostileJSON)
	if err != nil {
		t.Fatalf("failed to call management.handle: %v", err)
	}
	var envHostile plugin.Envelope
	_ = json.Unmarshal(rawHostile, &envHostile)
	var respHostile plugin.ManagementResponsePayload
	_ = json.Unmarshal(envHostile.Result, &respHostile)
	if respHostile.StatusCode != 400 {
		t.Errorf("expected status code 400 for directory traversal, got %d", respHostile.StatusCode)
	}

	// 7. Valid embedded JS file returns 200 with application/javascript
	var jsRelPath string
	_ = fs.WalkDir(web.AssetsFS, "assets", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr == nil && !d.IsDir() && strings.HasSuffix(p, ".js") {
			jsRelPath = strings.TrimPrefix(p, "assets/")
			return fs.SkipAll
		}
		return nil
	})
	if jsRelPath != "" {
		jsReqJSON := []byte(fmt.Sprintf(`{"Method":"GET","Path":"/v0/resource/plugins/visual-profile/profiles/%s"}`, jsRelPath))
		rawJS, err := plugin.HandlePluginMethod("management.handle", jsReqJSON)
		if err != nil {
			t.Fatalf("failed to fetch embedded JS: %v", err)
		}
		var envJS plugin.Envelope
		_ = json.Unmarshal(rawJS, &envJS)
		var respJS plugin.ManagementResponsePayload
		_ = json.Unmarshal(envJS.Result, &respJS)
		if respJS.StatusCode != 200 {
			t.Errorf("expected status 200 for valid embedded JS, got %d", respJS.StatusCode)
		}
		if len(respJS.Headers["Content-Type"]) == 0 || !strings.Contains(respJS.Headers["Content-Type"][0], "javascript") {
			t.Errorf("expected javascript content-type, got %v", respJS.Headers["Content-Type"])
		}
	}
}
