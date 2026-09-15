package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Antigravity quota endpoints in fallback priority order
var AntigravityQuotaEndpoints = []string{
	"https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary",
	"https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:retrieveUserQuotaSummary",
	"https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary",
}

// Client interacts with the CLIProxyAPI Management API.
type Client struct {
	baseURL    string
	mgmtKey    string
	httpClient *http.Client
}

// New creates a new Management API client.
func New(endpoint, mgmtKey string) (*Client, error) {
	clean := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if clean == "" {
		return nil, fmt.Errorf("empty endpoint")
	}
	u, err := url.Parse(clean)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("endpoint must use http or https scheme: %s", clean)
	}

	return &Client{
		baseURL: clean,
		mgmtKey: strings.TrimSpace(mgmtKey),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// AuthFileEntry represents an entry returned by GET /v0/management/auth-files.
type AuthFileEntry struct {
	ID        string `json:"id"`
	AuthIndex string `json:"auth_index"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Provider  string `json:"provider"`
	Status    string `json:"status"`
	Disabled  bool   `json:"disabled"`
	Email     string `json:"email"`
	ProjectID string `json:"project_id"`
}

type listAuthFilesResponse struct {
	Files []AuthFileEntry `json:"files"`
}

// ListAuthFiles retrieves all configured auth files.
func (c *Client) ListAuthFiles(ctx context.Context) ([]AuthFileEntry, error) {
	reqURL := fmt.Sprintf("%s/v0/management/auth-files", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list auth files failed (%d): %s", resp.StatusCode, string(body))
	}

	var res listAuthFilesResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("decode auth files response: %w", err)
	}

	return res.Files, nil
}

// GetAuthFilePrefix downloads the raw credential JSON and extracts only the "prefix" field.
// Secrets are discarded immediately from memory and never logged or persisted.
func (c *Client) GetAuthFilePrefix(ctx context.Context, name string) (string, error) {
	reqURL := fmt.Sprintf("%s/v0/management/auth-files/download?name=%s", c.baseURL, url.QueryEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("create download request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("execute download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("download auth file failed (%d): %s", resp.StatusCode, string(body))
	}

	var rawMap map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rawMap); err != nil {
		return "", fmt.Errorf("decode auth file JSON: %w", err)
	}

	// Extract prefix if present
	if prefixVal, ok := rawMap["prefix"]; ok && prefixVal != nil {
		if prefixStr, isStr := prefixVal.(string); isStr {
			return strings.TrimSpace(prefixStr), nil
		}
	}

	return "", nil
}

// APICallRequest mirrors the body expected by POST /v0/management/api-call.
type APICallRequest struct {
	AuthIndex string            `json:"authIndex"`
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Header    map[string]string `json:"header,omitempty"`
	Data      string            `json:"data,omitempty"`
}

// APICallResponse envelopes the payload returned by /v0/management/api-call.
type APICallResponse struct {
	StatusCode int                 `json:"status_code"`
	Header     map[string][]string `json:"header"`
	Body       any                 `json:"body"`
}

// GetQuotaSummary calls Antigravity's retrieveUserQuotaSummary via the management api-call proxy.
func (c *Client) GetQuotaSummary(ctx context.Context, authIndex, projectID string) ([]byte, error) {
	var lastErr error

	for _, endpoint := range AntigravityQuotaEndpoints {
		payloadData := "{}"
		if strings.TrimSpace(projectID) != "" {
			payloadData = fmt.Sprintf(`{"project":%q}`, strings.TrimSpace(projectID))
		}

		apiReq := APICallRequest{
			AuthIndex: authIndex,
			Method:    http.MethodPost,
			URL:       endpoint,
			Header: map[string]string{
				"Authorization": "Bearer $TOKEN$",
				"Content-Type":  "application/json",
				"User-Agent":    "antigravity/cli/1.0.13 (aidev_client; os_type=darwin; arch=arm64)",
			},
			Data: payloadData,
		}

		bodyBytes, err := json.Marshal(apiReq)
		if err != nil {
			return nil, fmt.Errorf("marshal api-call request: %w", err)
		}

		reqURL := fmt.Sprintf("%s/v0/management/api-call", c.baseURL)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("create api-call request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		c.setHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("endpoint %s failed: %w", endpoint, err)
			continue
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

		if readErr != nil {
			lastErr = fmt.Errorf("read response body from %s: %w", endpoint, readErr)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("management endpoint %s returned %d: %s", endpoint, resp.StatusCode, string(respBody))
			continue
		}

		// Parse the management proxy response envelope
		var apiCallResp APICallResponse
		if err := json.Unmarshal(respBody, &apiCallResp); err != nil {
			// If not an envelope, use raw bytes directly
			return respBody, nil
		}

		// Check upstream status code inside the envelope
		if apiCallResp.StatusCode > 0 && (apiCallResp.StatusCode < 200 || apiCallResp.StatusCode >= 300) {
			lastErr = fmt.Errorf("upstream returned %d: %v", apiCallResp.StatusCode, apiCallResp.Body)
			continue
		}

		// Extract the actual upstream body
		switch b := apiCallResp.Body.(type) {
		case string:
			return []byte(b), nil
		case nil:
			return respBody, nil
		default:
			marshaled, errMarshal := json.Marshal(b)
			if errMarshal != nil {
				return nil, fmt.Errorf("marshal unwrapped body: %w", errMarshal)
			}
			return marshaled, nil
		}
	}

	return nil, fmt.Errorf("all antigravity quota endpoints failed: %w", lastErr)
}

// PatchAuthPrefix updates the prefix attribute of an auth record using PATCH /v0/management/auth-files/fields.
func (c *Client) PatchAuthPrefix(ctx context.Context, nameOrID, newPrefix string) error {
	payload := map[string]string{
		"name":   strings.TrimSpace(nameOrID),
		"prefix": strings.TrimSpace(newPrefix),
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal patch payload: %w", err)
	}

	reqURL := fmt.Sprintf("%s/v0/management/auth-files/fields", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create patch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute patch request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("patch auth prefix failed (%d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (c *Client) setHeaders(req *http.Request) {
	if c.mgmtKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.mgmtKey)
		req.Header.Set("X-Management-Key", c.mgmtKey)
	}
}
