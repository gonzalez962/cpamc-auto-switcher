package plugin

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"

	"visual-profile/internal/handlers"
	"visual-profile/internal/version"
	"visual-profile/internal/web"
)

// ABIVersion defines the supported CLIProxyAPI C-ABI version.
const ABIVersion uint32 = 1

// Envelope wraps all method responses in the standard CLIProxyAPI JSON protocol.
type Envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *EnvelopeError  `json:"error,omitempty"`
}

// EnvelopeError represents an error returned in the standard envelope.
type EnvelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ManagementRequestPayload represents an incoming HTTP request forwarded by CLIProxyAPI host.
type ManagementRequestPayload struct {
	Method  string              `json:"Method"`
	Path    string              `json:"Path"`
	Headers map[string][]string `json:"Headers"`
	Query   map[string][]string `json:"Query"`
	Body    []byte              `json:"Body"`
}

// ManagementResponsePayload represents the HTTP response returned to CLIProxyAPI host.
type ManagementResponsePayload struct {
	StatusCode int                 `json:"StatusCode"`
	Headers    map[string][]string `json:"Headers"`
	Body       string              `json:"Body"` // Base64 encoded payload body
}

// HandlePluginMethod dispatches incoming CLIProxyAPI plugin method calls.
func HandlePluginMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		registration := map[string]any{
			"schema_version": 6,
			"metadata": map[string]any{
				"Name":             "visual-profile",
				"Version":          version.Version,
				"Author":           "gonzalez962",
				"Description":      "Visual profile prefix management plugin with React Flow editor",
				"GitHubRepository": "https://github.com/gonzalez962/cpamc-auto-switcher",
				"Logo":             "",
				"ConfigFields":     []any{},
			},
			"capabilities": map[string]any{
				"management_api": true,
			},
		}
		raw, err := json.Marshal(registration)
		if err != nil {
			return nil, err
		}
		return OKEnvelope(raw), nil

	case "management.register":
		regResponse := map[string]any{
			"resources": []map[string]any{
				{
					"Path":        "/profiles",
					"Menu":        "Visual Profile",
					"Description": "Visual profile prefix management and topology editor",
				},
			},
		}
		raw, err := json.Marshal(regResponse)
		if err != nil {
			return nil, err
		}
		return OKEnvelope(raw), nil

	case "management.handle":
		return HandleManagementHTTP(request)

	default:
		return ErrorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

// MatchProfilesRoute validates that rawPath targets the exact '/profiles' segment
// and extracts the relative asset subpath.
// Rejects paths where 'profiles' is only a substring (e.g., '/profiles_evil', '/myprofiles').
func MatchProfilesRoute(rawPath string) (subpath string, matched bool) {
	if rawPath == "" {
		return "", false
	}

	unescaped := rawPath
	if u, err := url.PathUnescape(rawPath); err == nil {
		unescaped = u
	}

	// Normalize Windows backslashes
	normalized := strings.ReplaceAll(unescaped, "\\", "/")

	// Strip query string and fragment if present in raw path
	if idx := strings.IndexAny(normalized, "?#"); idx != -1 {
		normalized = normalized[:idx]
	}

	// Split by '/' to inspect exact path segments
	segments := strings.Split(normalized, "/")

	profilesIdx := -1
	for i, seg := range segments {
		if seg == "profiles" {
			profilesIdx = i
			break
		}
	}

	if profilesIdx == -1 {
		return "", false
	}

	subSegments := segments[profilesIdx+1:]
	rawSub := strings.Join(subSegments, "/")
	rawSub = strings.TrimPrefix(rawSub, "/")
	rawSub = strings.TrimSpace(rawSub)

	if rawSub == "" {
		rawSub = "index.html"
	}

	return rawSub, true
}

// HandleManagementHTTP resolves asset requests under the /profiles route and serves embedded files.
// Enforces exact /profiles segment routing, validates against directory traversal, and restricts to GET/HEAD.
func HandleManagementHTTP(request []byte) ([]byte, error) {
	var req ManagementRequestPayload
	if len(request) > 0 {
		if err := json.Unmarshal(request, &req); err != nil {
			resp := ManagementResponsePayload{
				StatusCode: http.StatusBadRequest,
				Headers: map[string][]string{
					"Content-Type":           {"application/json; charset=utf-8"},
					"X-Content-Type-Options": {"nosniff"},
				},
				Body: base64.StdEncoding.EncodeToString([]byte(`{"error":"bad_request","message":"malformed JSON request payload"}`)),
			}
			raw, _ := json.Marshal(resp)
			return OKEnvelope(raw), nil
		}
	}

	// 1. Method guard: only GET and HEAD are permitted on static management resources
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet // default when omitted
	}
	if method != http.MethodGet && method != http.MethodHead {
		resp := ManagementResponsePayload{
			StatusCode: http.StatusMethodNotAllowed,
			Headers: map[string][]string{
				"Content-Type":           {"application/json; charset=utf-8"},
				"X-Content-Type-Options": {"nosniff"},
			},
			Body: base64.StdEncoding.EncodeToString([]byte(`{"error":"method_not_allowed","message":"method not allowed"}`)),
		}
		raw, _ := json.Marshal(resp)
		return OKEnvelope(raw), nil
	}

	// 2. Exact segment-scoped routing: must match '/profiles' as an exact path segment
	subpath, matched := MatchProfilesRoute(req.Path)
	if !matched {
		resp := ManagementResponsePayload{
			StatusCode: http.StatusNotFound,
			Headers: map[string][]string{
				"Content-Type":           {"application/json; charset=utf-8"},
				"X-Content-Type-Options": {"nosniff"},
			},
			Body: base64.StdEncoding.EncodeToString([]byte(`{"error":"not_found","message":"resource not found"}`)),
		}
		raw, _ := json.Marshal(resp)
		return OKEnvelope(raw), nil
	}

	// 3. Security guard: detect directory traversal attempts in path and subpath
	unescapedReqPath := req.Path
	if u, err := url.PathUnescape(req.Path); err == nil {
		unescapedReqPath = u
	}
	if strings.Contains(req.Path, "..") ||
		strings.Contains(unescapedReqPath, "..") ||
		strings.Contains(subpath, "..") ||
		strings.Contains(subpath, "\x00") ||
		strings.Contains(req.Path, "\x00") {
		resp := ManagementResponsePayload{
			StatusCode: http.StatusBadRequest,
			Headers: map[string][]string{
				"Content-Type":           {"application/json; charset=utf-8"},
				"X-Content-Type-Options": {"nosniff"},
			},
			Body: base64.StdEncoding.EncodeToString([]byte(`{"error":"invalid_path","message":"invalid or unsafe path"}`)),
		}
		raw, _ := json.Marshal(resp)
		return OKEnvelope(raw), nil
	}

	// Clean and normalize subpath for asset retrieval
	cleanSubpath := path.Clean(strings.ReplaceAll(subpath, "\\", "/"))
	cleanSubpath = strings.TrimPrefix(cleanSubpath, "/")
	if cleanSubpath == "" || cleanSubpath == "." {
		cleanSubpath = "index.html"
	}

	// 4. Retrieve asset from embedded storage
	assetBytes, contentType, err := web.GetAsset(cleanSubpath)
	if err != nil {
		statusCode := http.StatusNotFound
		errCode := "not_found"
		errMsg := "resource not found"

		if errors.Is(err, web.ErrInvalidPath) {
			statusCode = http.StatusBadRequest
			errCode = "invalid_path"
			errMsg = "invalid or unsafe asset path"
		}

		resp := ManagementResponsePayload{
			StatusCode: statusCode,
			Headers: map[string][]string{
				"Content-Type":           {"application/json; charset=utf-8"},
				"X-Content-Type-Options": {"nosniff"},
			},
			Body: base64.StdEncoding.EncodeToString([]byte(`{"error":"` + errCode + `","message":"` + errMsg + `"}`)),
		}
		raw, _ := json.Marshal(resp)
		return OKEnvelope(raw), nil
	}

	headers := handlers.DefaultSecurityHeaders(contentType)

	// For HEAD requests, response headers must match but body must be empty
	var bodyEncoded string
	if method != http.MethodHead {
		bodyEncoded = base64.StdEncoding.EncodeToString(assetBytes)
	}

	resp := ManagementResponsePayload{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       bodyEncoded,
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}
	return OKEnvelope(raw), nil
}

// OKEnvelope creates a successful Envelope with raw JSON result.
func OKEnvelope(result []byte) []byte {
	raw, _ := json.Marshal(Envelope{OK: true, Result: json.RawMessage(result)})
	return raw
}

// ErrorEnvelope creates an error Envelope with specified code and message.
func ErrorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(Envelope{OK: false, Error: &EnvelopeError{Code: code, Message: message}})
	return raw
}
