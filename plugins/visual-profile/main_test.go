package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
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
	expectedBaseTag := `<base href="/v0/resource/plugins/visual-profile/profiles/">`
	if !strings.Contains(indexHTML, expectedBaseTag) {
		t.Errorf("expected index HTML to contain base tag %q", expectedBaseTag)
	}
	baseIdx := strings.Index(indexHTML, "<base ")
	scriptIdx := strings.Index(indexHTML, "<script ")
	linkIdx := strings.Index(indexHTML, "<link ")
	if baseIdx == -1 || scriptIdx == -1 || linkIdx == -1 {
		t.Errorf("expected <base>, <script>, and <link> tags in HTML")
	} else {
		if baseIdx > scriptIdx {
			t.Errorf("expected <base> before <script>")
		}
		if baseIdx > linkIdx {
			t.Errorf("expected <base> before <link>")
		}
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

	// 8. Valid embedded CSS file returns 200 with text/css
	var cssRelPath string
	_ = fs.WalkDir(web.AssetsFS, "assets", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr == nil && !d.IsDir() && strings.HasSuffix(p, ".css") {
			cssRelPath = strings.TrimPrefix(p, "assets/")
			return fs.SkipAll
		}
		return nil
	})
	if cssRelPath != "" {
		cssReqJSON := []byte(fmt.Sprintf(`{"Method":"GET","Path":"/v0/resource/plugins/visual-profile/profiles/%s"}`, cssRelPath))
		rawCSS, err := plugin.HandlePluginMethod("management.handle", cssReqJSON)
		if err != nil {
			t.Fatalf("failed to fetch embedded CSS: %v", err)
		}
		var envCSS plugin.Envelope
		_ = json.Unmarshal(rawCSS, &envCSS)
		var respCSS plugin.ManagementResponsePayload
		_ = json.Unmarshal(envCSS.Result, &respCSS)
		if respCSS.StatusCode != 200 {
			t.Errorf("expected status 200 for valid embedded CSS, got %d", respCSS.StatusCode)
		}
		if len(respCSS.Headers["Content-Type"]) == 0 || !strings.Contains(respCSS.Headers["Content-Type"][0], "text/css") {
			t.Errorf("expected text/css content-type, got %v", respCSS.Headers["Content-Type"])
		}
	}

	// 9. URL resolution regression verification
	// Verifies that relative asset paths resolve under /profiles/assets/ via injected base tag
	// when requested without trailing slash.
	reqWithoutSlash := []byte(`{"Method":"GET","Path":"/v0/resource/plugins/visual-profile/profiles"}`)
	rawWithoutSlash, err := plugin.HandlePluginMethod("management.handle", reqWithoutSlash)
	if err != nil {
		t.Fatalf("failed to fetch HTML without trailing slash: %v", err)
	}
	var envWithoutSlash plugin.Envelope
	_ = json.Unmarshal(rawWithoutSlash, &envWithoutSlash)
	var respWithoutSlash plugin.ManagementResponsePayload
	_ = json.Unmarshal(envWithoutSlash.Result, &respWithoutSlash)
	rawHTMLBytes, _ := base64.StdEncoding.DecodeString(respWithoutSlash.Body)
	rawHTML := string(rawHTMLBytes)

	baseRegex := regexp.MustCompile(`<base\s+href="([^"]+)"`)
	baseMatches := baseRegex.FindStringSubmatch(rawHTML)
	if len(baseMatches) < 2 {
		t.Fatalf("base tag missing in response HTML")
	}
	baseURL, err := url.Parse(baseMatches[1])
	if err != nil {
		t.Fatalf("failed to parse base href: %v", err)
	}

	linkRegex := regexp.MustCompile(`<link\b[^>]*\bhref="([^"]+)"`)
	linkMatch := linkRegex.FindStringSubmatch(rawHTML)
	if len(linkMatch) >= 2 {
		relCSS, err := url.Parse(linkMatch[1])
		if err != nil {
			t.Fatalf("failed to parse CSS href: %v", err)
		}
		resolvedCSS := baseURL.ResolveReference(relCSS).Path
		if !strings.HasPrefix(resolvedCSS, "/v0/resource/plugins/visual-profile/profiles/assets/") {
			t.Errorf("resolved CSS path %q must be under /profiles/assets/", resolvedCSS)
		}
		cssJSON := []byte(fmt.Sprintf(`{"Method":"GET","Path":"%s"}`, resolvedCSS))
		rawCSSRes, _ := plugin.HandlePluginMethod("management.handle", cssJSON)
		var envCSSRes plugin.Envelope
		_ = json.Unmarshal(rawCSSRes, &envCSSRes)
		var respCSSRes plugin.ManagementResponsePayload
		_ = json.Unmarshal(envCSSRes.Result, &respCSSRes)
		if respCSSRes.StatusCode != 200 {
			t.Errorf("expected status 200 for resolved CSS asset %q, got %d", resolvedCSS, respCSSRes.StatusCode)
		}
	}
}
