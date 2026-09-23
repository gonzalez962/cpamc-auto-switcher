package web

import (
	"errors"
	"io/fs"
	"regexp"
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

	if len(data) < 100000 {
		t.Errorf("expected self-contained single-file HTML to be >100KB, got %d bytes", len(data))
	}

	if !strings.Contains(mimeType, "text/html") {
		t.Errorf("expected text/html mime type, got %q", mimeType)
	}

	htmlStr := string(data)
	if !strings.Contains(htmlStr, "<div id=\"root\">") && !strings.Contains(htmlStr, "<html") {
		t.Errorf("expected HTML structure in index.html, got: %s", htmlStr[:200])
	}

	// VP-6: Verify self-contained single-file build: inline <style> and <script>, no external CSS/JS requests
	if !strings.Contains(htmlStr, "<style") {
		t.Errorf("expected inline <style> tag in index.html")
	}
	if !strings.Contains(htmlStr, "<script") {
		t.Errorf("expected inline <script> tag in index.html")
	}

	scriptSrcRegex := regexp.MustCompile(`(?i)<script\b[^>]*\bsrc\s*=`)
	if scriptSrcRegex.MatchString(htmlStr) {
		t.Errorf("expected single-file index.html to have no script src, found external script reference")
	}

	stylesheetRegex := regexp.MustCompile(`(?i)<link\b[^>]*\b(rel\s*=\s*["']?stylesheet|href\s*=\s*["'][^"']*\.css)`)
	if stylesheetRegex.MatchString(htmlStr) {
		t.Errorf("expected single-file index.html to have no external stylesheet link, found match")
	}

	if strings.Contains(htmlStr, "<base ") {
		t.Errorf("expected index.html to not contain obsolete <base> tag")
	}

	// Verify no external asset requests, modulepreload polyfill, or icon links
	if strings.Contains(strings.ToLower(htmlStr), "modulepreload") {
		t.Errorf("expected Vite modulePreload polyfill to be disabled, found modulepreload reference")
	}
	iconRegex := regexp.MustCompile(`(?i)<link\b[^>]*\brel\s*=\s*["']?(shortcut )?icon["']?`)
	if iconRegex.MatchString(htmlStr) {
		t.Errorf("expected no icon link in index.html, found: %s", iconRegex.FindString(htmlStr))
	}
}

func TestGetAsset_NoExternalAssetRequestsOrModulePreloadPolyfill(t *testing.T) {
	data, _, err := GetAsset("index.html")
	if err != nil {
		t.Fatalf("failed to read index.html: %v", err)
	}
	htmlStr := string(data)

	// 1. Vite modulePreload polyfill disabled to remove dead fetch
	if strings.Contains(htmlStr, `supports("modulepreload")`) || strings.Contains(htmlStr, `rel="modulepreload"`) {
		t.Errorf("expected Vite modulePreload polyfill to be absent, found modulepreload polyfill code")
	}

	// 2. No external asset requests (<link rel="icon">, external stylesheets, external scripts)
	linkRegex := regexp.MustCompile(`(?i)<link\b[^>]*>`)
	if linkRegex.MatchString(htmlStr) {
		t.Errorf("expected zero <link> tags in single-file index.html, found: %s", linkRegex.FindString(htmlStr))
	}

	externalScriptRegex := regexp.MustCompile(`(?i)<script\b[^>]*\bsrc\s*=`)
	if externalScriptRegex.MatchString(htmlStr) {
		t.Errorf("expected zero external <script src> in single-file index.html, found: %s", externalScriptRegex.FindString(htmlStr))
	}

	// 3. Single HTML file embed verification: AssetsFS contains only index.html
	var fileNames []string
	walkErr := fs.WalkDir(AssetsFS, "assets", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			fileNames = append(fileNames, path)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("failed to walk AssetsFS: %v", walkErr)
	}
	if len(fileNames) != 1 || fileNames[0] != "assets/index.html" {
		t.Errorf("expected exactly [assets/index.html] embedded in AssetsFS, got %v", fileNames)
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
