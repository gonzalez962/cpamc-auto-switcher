package web

import (
	"embed"
	"errors"
	"io/fs"
	"mime"
	"path"
	"strings"
)

//go:embed assets/*
var AssetsFS embed.FS

// ErrAssetNotFound is returned when the requested asset does not exist in embedded storage.
var ErrAssetNotFound = errors.New("asset not found")

// ErrInvalidPath is returned when the path is malformed or attempts directory traversal.
var ErrInvalidPath = errors.New("invalid or unsafe asset path")

// GetAsset reads a file from embedded assets by name (e.g., "index.html" or "assets/index.js").
// Returns raw file content, resolved MIME content-type, and error if not found or unsafe.
func GetAsset(assetName string) ([]byte, string, error) {
	normalized := strings.ReplaceAll(strings.TrimSpace(assetName), "\\", "/")
	cleanName := path.Clean(normalized)
	cleanName = strings.TrimPrefix(cleanName, "/")

	// Guard against path traversal attempts, null bytes, or empty paths
	if cleanName == "." || cleanName == "" || strings.HasPrefix(cleanName, "..") || strings.Contains(cleanName, "..") || strings.Contains(cleanName, "\x00") {
		return nil, "", ErrInvalidPath
	}

	fullPath := path.Join("assets", cleanName)
	data, err := fs.ReadFile(AssetsFS, fullPath)
	if err != nil {
		return nil, "", ErrAssetNotFound
	}

	mimeType := ResolveMIMEType(cleanName)
	return data, mimeType, nil
}

// ResolveMIMEType determines the Content-Type header value based on file extension.
func ResolveMIMEType(filename string) string {
	ext := strings.ToLower(path.Ext(filename))
	switch ext {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".ico":
		return "image/x-icon"
	case ".woff2":
		return "font/woff2"
	case ".woff":
		return "font/woff"
	default:
		detected := mime.TypeByExtension(ext)
		if detected != "" {
			return detected
		}
		return "application/octet-stream"
	}
}
