package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestResourceHandler_ServeIndex(t *testing.T) {
	handler := NewResourceHandler()

	paths := []string{
		DefaultProfilesBasePath,
		DefaultProfilesBasePath + "/",
		DefaultProfilesBasePath + "/index.html",
	}

	scriptSrcRegex := regexp.MustCompile(`(?i)<script\b[^>]*\bsrc\s*=`)
	stylesheetRegex := regexp.MustCompile(`(?i)<link\b[^>]*\b(rel\s*=\s*["']?stylesheet|href\s*=\s*["'][^"']*\.css)`)

	for _, p := range paths {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK for %q, got %d", p, rec.Code)
		}

		contentType := rec.Header().Get("Content-Type")
		if !strings.Contains(contentType, "text/html") {
			t.Errorf("expected text/html for %q, got %q", p, contentType)
		}

		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("expected nosniff header for %q", p)
		}

		bodyStr := rec.Body.String()
		if len(bodyStr) < 100000 {
			t.Errorf("expected single-file index HTML >100KB for %q, got %d bytes", p, len(bodyStr))
		}
		if !strings.Contains(bodyStr, "<style") {
			t.Errorf("expected inline <style> tag for %q", p)
		}
		if !strings.Contains(bodyStr, "<script") {
			t.Errorf("expected inline <script> tag for %q", p)
		}
		if scriptSrcRegex.MatchString(bodyStr) {
			t.Errorf("expected no script src for %q", p)
		}
		if stylesheetRegex.MatchString(bodyStr) {
			t.Errorf("expected no stylesheet link for %q", p)
		}
		if strings.Contains(strings.ToLower(bodyStr), "modulepreload") {
			t.Errorf("expected Vite modulePreload polyfill to be disabled for %q", p)
		}
		linkRegex := regexp.MustCompile(`(?i)<link\b[^>]*>`)
		if linkRegex.MatchString(bodyStr) {
			t.Errorf("expected zero <link> tags in single-file index HTML for %q, found: %s", p, linkRegex.FindString(bodyStr))
		}
	}
}

// TestResourceHandler_HostSlashlessVsInternalIsolation verifies:
// - Host-facing runtime routing is strictly slashless "/profiles" (the only path supported by CLIProxyAPI host resourceRoutes).
// - In isolation, ResourceHandler also defensively tolerates trailing slash variants.
func TestResourceHandler_HostSlashlessVsInternalIsolation(t *testing.T) {
	handler := NewResourceHandlerWithPath("/profiles")

	paths := []string{
		"/profiles",            // Host registered slashless route (only supported host runtime path)
		"/profiles/",           // Internal isolation trailing slash fallback
		"/profiles/index.html", // Internal isolation index.html fallback
	}

	for _, p := range paths {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK for %q, got %d", p, rec.Code)
		}

		contentType := rec.Header().Get("Content-Type")
		if !strings.Contains(contentType, "text/html") {
			t.Errorf("expected text/html for %q, got %q", p, contentType)
		}

		bodyStr := rec.Body.String()
		if !strings.Contains(bodyStr, `<div id="root">`) {
			t.Errorf("expected root div for %q", p)
		}
	}
}

func TestResourceHandler_MethodNotAllowed(t *testing.T) {
	handler := NewResourceHandler()
	req := httptest.NewRequest(http.MethodPost, DefaultProfilesBasePath, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed, got %d", rec.Code)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error json: %v", err)
	}
	if errResp.Error != "method not allowed" {
		t.Errorf("expected 'method not allowed', got %q", errResp.Error)
	}
}

func TestResourceHandler_NotFoundAndTraversal(t *testing.T) {
	handler := NewResourceHandler()

	badRequests := []string{
		"/v0/resource/plugins/other-plugin/profiles",
		DefaultProfilesBasePath + "/nonexistent-asset-1234.js",
		DefaultProfilesBasePath + "/../../secret.txt",
	}

	for _, p := range badRequests {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404 for %q, got %d", p, rec.Code)
		}
	}
}

func TestResourceHandler_HeadMethod(t *testing.T) {
	handler := NewResourceHandler()
	req := httptest.NewRequest(http.MethodHead, DefaultProfilesBasePath, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for HEAD, got %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("expected empty body for HEAD, got %d bytes", rec.Body.Len())
	}
}
