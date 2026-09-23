package web

import (
	"errors"
	"strings"
	"testing"
)

func TestGetAsset_IndexHtml(t *testing.T) {
	data, mimeType, err := GetAsset("index.html")
	if err != nil {
		t.Fatalf("expected index.html to be found in embedded assets, got error: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("expected non-empty index.html content")
	}

	if !strings.Contains(mimeType, "text/html") {
		t.Errorf("expected text/html mime type, got %q", mimeType)
	}

	if !strings.Contains(string(data), "<div id=\"root\">") && !strings.Contains(string(data), "<html") {
		t.Errorf("expected HTML structure in index.html, got: %s", string(data))
	}
}

func TestGetAsset_PathTraversalGuard(t *testing.T) {
	badPaths := []string{
		"../main.go",
		"../../go.mod",
		"assets/../../secret.txt",
		"/../internal",
		"",
		".",
	}

	for _, p := range badPaths {
		_, _, err := GetAsset(p)
		if err == nil {
			t.Errorf("expected error for path traversal attempt %q, got nil", p)
		}
		if !errors.Is(err, ErrInvalidPath) && !errors.Is(err, ErrAssetNotFound) {
			t.Errorf("expected ErrInvalidPath or ErrAssetNotFound for %q, got: %v", p, err)
		}
	}
}

func TestGetAsset_NotFound(t *testing.T) {
	_, _, err := GetAsset("nonexistent-file.xyz")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
	if !errors.Is(err, ErrAssetNotFound) {
		t.Errorf("expected ErrAssetNotFound, got: %v", err)
	}
}

func TestResolveMIMEType(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"index.html", "text/html; charset=utf-8"},
		{"style.css", "text/css; charset=utf-8"},
		{"bundle.js", "application/javascript; charset=utf-8"},
		{"module.mjs", "application/javascript; charset=utf-8"},
		{"data.json", "application/json; charset=utf-8"},
		{"icon.svg", "image/svg+xml"},
		{"logo.png", "image/png"},
		{"photo.jpg", "image/jpeg"},
		{"favicon.ico", "image/x-icon"},
		{"font.woff2", "font/woff2"},
		{"font.woff", "font/woff"},
		{"binary.unknownext123", "application/octet-stream"},
	}

	for _, tt := range tests {
		got := ResolveMIMEType(tt.filename)
		if got != tt.expected {
			t.Errorf("ResolveMIMEType(%q) = %q, expected %q", tt.filename, got, tt.expected)
		}
	}
}
