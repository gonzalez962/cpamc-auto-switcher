package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
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
			Name             string `json:"Name"`
			Version          string `json:"Version"`
			Author           string `json:"Author"`
			GitHubRepository string `json:"GitHubRepository"`
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
	if payload.Metadata.Author != "gonzalez962" {
		t.Errorf("expected author 'gonzalez962', got %q", payload.Metadata.Author)
	}
	if payload.Metadata.GitHubRepository != "https://github.com/gonzalez962/cpamc-auto-switcher" {
		t.Errorf("expected GitHubRepository 'https://github.com/gonzalez962/cpamc-auto-switcher', got %q", payload.Metadata.GitHubRepository)
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

	indexBodyBytes, err := base64.StdEncoding.DecodeString(respPayload.Body)
	if err != nil {
		t.Fatalf("failed to decode index HTML body: %v", err)
	}
	indexHTML := string(indexBodyBytes)
	if !strings.Contains(indexHTML, `<div id="root">`) {
		t.Errorf("expected index HTML to contain root div")
	}
	if !strings.Contains(indexHTML, "<style") {
		t.Errorf("expected index HTML to contain inline <style>")
	}
	if !strings.Contains(indexHTML, "<script") {
		t.Errorf("expected index HTML to contain inline <script>")
	}
	scriptSrcRegex := regexp.MustCompile(`(?i)<script\b[^>]*\bsrc\s*=`)
	if scriptSrcRegex.MatchString(indexHTML) {
		t.Errorf("expected single-file index HTML to have no script src, found match")
	}
	linkCSSRegex := regexp.MustCompile(`(?i)<link\b[^>]*\b(rel\s*=\s*["']?stylesheet|href\s*=\s*["'][^"']*\.css)`)
	if linkCSSRegex.MatchString(indexHTML) {
		t.Errorf("expected single-file index HTML to have no stylesheet link, found match")
	}
	if strings.Contains(indexHTML, "<base ") {
		t.Errorf("expected no obsolete <base> tag in single-file index HTML")
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

	// 7. Host resource routing: Only slashless /profiles is registered and supported by host runtime routing.
	// Internal handler also tolerates trailing slash defensively in isolation.
	profilesRoutes := []string{
		"/profiles",                                    // Host registered slashless route (only supported host runtime path)
		"/profiles/",                                   // Internal isolation fallback
		"/v0/resource/plugins/visual-profile/profiles",  // Internal isolation plugin path
		"/v0/resource/plugins/visual-profile/profiles/", // Internal isolation plugin path with trailing slash
	}
	for _, p := range profilesRoutes {
		routeReqJSON := []byte(fmt.Sprintf(`{"Method":"GET","Path":"%s"}`, p))
		rawRoute, err := plugin.HandlePluginMethod("management.handle", routeReqJSON)
		if err != nil {
			t.Fatalf("failed to fetch HTML for %s: %v", p, err)
		}
		var envRoute plugin.Envelope
		_ = json.Unmarshal(rawRoute, &envRoute)
		var respRoute plugin.ManagementResponsePayload
		_ = json.Unmarshal(envRoute.Result, &respRoute)
		if respRoute.StatusCode != 200 {
			t.Errorf("expected status 200 for %s, got %d", p, respRoute.StatusCode)
		}
		rawBytes, _ := base64.StdEncoding.DecodeString(respRoute.Body)
		if len(rawBytes) == 0 {
			t.Errorf("expected non-empty body for %s", p)
		}
	}

	// 8. Single-file build verification: embed includes inline build with no subresource dependencies
	assetData, mimeType, err := web.GetAsset("index.html")
	if err != nil {
		t.Fatalf("failed to get index.html from embedded web assets: %v", err)
	}
	if !strings.Contains(mimeType, "text/html") {
		t.Errorf("expected text/html for index.html, got %q", mimeType)
	}
	if len(assetData) < 100000 {
		t.Errorf("expected inline bundle >100KB, got %d bytes", len(assetData))
	}
	assetHTML := string(assetData)
	if !strings.Contains(assetHTML, "<style") || !strings.Contains(assetHTML, "<script") {
		t.Errorf("expected inline style and script tags in embedded index.html")
	}
	if strings.Contains(strings.ToLower(assetHTML), "modulepreload") {
		t.Errorf("expected Vite modulePreload polyfill to be disabled in embedded index.html")
	}
	linkRegex := regexp.MustCompile(`(?i)<link\b[^>]*>`)
	if linkRegex.MatchString(assetHTML) {
		t.Errorf("expected zero <link> tags in single-file embedded index.html, found: %s", linkRegex.FindString(assetHTML))
	}

	// 9. External subresource rejection: separate CSS/JS subpaths return 404
	subresourceJSON := []byte(`{"Method":"GET","Path":"/v0/resource/plugins/visual-profile/profiles/assets/index.js"}`)
	rawSub, err := plugin.HandlePluginMethod("management.handle", subresourceJSON)
	if err != nil {
		t.Fatalf("failed to call management.handle: %v", err)
	}
	var envSub plugin.Envelope
	_ = json.Unmarshal(rawSub, &envSub)
	var respSub plugin.ManagementResponsePayload
	_ = json.Unmarshal(envSub.Result, &respSub)
	if respSub.StatusCode != 404 {
		t.Errorf("expected status 404 for separate asset subresource request, got %d", respSub.StatusCode)
	}
}
