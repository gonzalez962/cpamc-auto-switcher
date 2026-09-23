package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
