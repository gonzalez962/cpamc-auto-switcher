package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientListAuthFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/auth-files" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing or invalid Authorization header: %s", r.Header.Get("Authorization"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files": []map[string]any{
				{
					"id":         "account-1",
					"auth_index": "idx-1",
					"provider":   "antigravity",
					"disabled":   false,
				},
			},
		})
	}))
	defer server.Close()

	cli, err := New(server.URL, "test-key")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	files, err := cli.ListAuthFiles(context.Background())
	if err != nil {
		t.Fatalf("ListAuthFiles error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].ID != "account-1" || files[0].Provider != "antigravity" {
		t.Errorf("unexpected file data: %+v", files[0])
	}
}

func TestClientGetAuthFilePrefix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/auth-files/download" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("name") != "account-1.json" {
			t.Errorf("unexpected query name: %s", r.URL.Query().Get("name"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type":          "antigravity",
			"access_token":  "topsecret",
			"refresh_token": "topsecret2",
			"prefix":        "agy",
		})
	}))
	defer server.Close()

	cli, err := New(server.URL, "test-key")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	prefix, err := cli.GetAuthFilePrefix(context.Background(), "account-1.json")
	if err != nil {
		t.Fatalf("GetAuthFilePrefix error: %v", err)
	}
	if prefix != "agy" {
		t.Errorf("expected prefix 'agy', got %q", prefix)
	}
}

func TestClientPatchAuthPrefix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/auth-files/fields" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPatch {
			t.Errorf("unexpected method: %s", r.Method)
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if body["name"] != "account-1" || body["prefix"] != "agy_new" {
			t.Errorf("unexpected patch body: %+v", body)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	cli, err := New(server.URL, "test-key")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	if err := cli.PatchAuthPrefix(context.Background(), "account-1", "agy_new"); err != nil {
		t.Fatalf("PatchAuthPrefix error: %v", err)
	}
}

func TestAuthFileManagementAuthStatusesAreManagementErrors(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("private management key detail"))
		}))
		cli, _ := New(server.URL, "key")
		_, err := cli.GetAuthFileDetails(context.Background(), "acct")
		server.Close()
		var managementErr *ManagementAPIError
		if !errors.As(err, &managementErr) || managementErr.StatusCode != status {
			t.Fatalf("status %d not classified as management auth failure: %v", status, err)
		}
		if strings.Contains(err.Error(), "private management key detail") {
			t.Fatalf("response body leaked: %v", err)
		}
	}
}

func TestManagementAndAuthFileFailuresAreTypedAndRedacted(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		status     int
	}{
		{"management api outage", "/v0/management/api-call", http.StatusServiceUnavailable},
		{"missing account metadata", "/v0/management/auth-files/download", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("private response body"))
			}))
			defer server.Close()
			cli, err := New(server.URL, "key")
			if err != nil {
				t.Fatal(err)
			}
			var got error
			if tc.path == "/v0/management/api-call" {
				_, got = cli.GetCodexQuotaSummary(context.Background(), "idx", "")
			} else {
				_, got = cli.GetAuthFileDetails(context.Background(), "missing")
			}
			var typed bool
			if tc.path == "/v0/management/api-call" {
				var target *ManagementAPIError
				typed = errors.As(got, &target)
			} else {
				var target *AuthFileError
				typed = errors.As(got, &target)
			}
			if got == nil || !typed {
				t.Fatalf("expected typed error, got %v", got)
			}
			if strings.Contains(got.Error(), "private response body") {
				t.Fatalf("sensitive body exposed: %v", got)
			}
		})
	}
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("private body read detail") }
func (failingBody) Close() error             { return nil }

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestQuotaResponseBodyReadFailureIsTypedTransportError(t *testing.T) {
	cli, err := New("http://proxy.invalid", "")
	if err != nil {
		t.Fatal(err)
	}
	cli.httpClient.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: failingBody{}}, nil
	})
	_, err = cli.GetCodexQuotaSummary(context.Background(), "idx", "")
	var transportErr *TransportError
	if !errors.As(err, &transportErr) {
		t.Fatalf("body read failure should be typed as transport failure: %v", err)
	}
	if strings.Contains(err.Error(), "private body read detail") {
		t.Fatalf("body read detail leaked: %v", err)
	}
}

func TestAntigravityFallbackPreservesManagementFailurePrecedence(t *testing.T) {
	original := AntigravityQuotaEndpoints
	AntigravityQuotaEndpoints = []string{"https://one.invalid", "https://two.invalid", "https://three.invalid"}
	defer func() { AntigravityQuotaEndpoints = original }()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("service secret"))
		case 2:
			_, _ = w.Write([]byte(`{"status_code":401,"body":"account detail"}`))
		default:
			_, _ = w.Write([]byte(`{"status_code":500,"body":"account detail"}`))
		}
	}))
	defer server.Close()
	cli, err := New(server.URL, "key")
	if err != nil {
		t.Fatal(err)
	}
	_, err = cli.GetQuotaSummary(context.Background(), "idx", "")
	var managementErr *ManagementAPIError
	if !errors.As(err, &managementErr) {
		t.Fatalf("management failure was masked by fallback error: %v", err)
	}
	if strings.Contains(err.Error(), "service secret") || strings.Contains(err.Error(), "account detail") {
		t.Fatalf("sensitive fallback response exposed: %v", err)
	}
}

func TestAntigravityFallbackPreservesTransportFailureOverAccountLocalFailure(t *testing.T) {
	original := AntigravityQuotaEndpoints
	AntigravityQuotaEndpoints = []string{"https://one.invalid", "https://two.invalid", "https://three.invalid"}
	defer func() { AntigravityQuotaEndpoints = original }()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			h, _ := w.(http.Hijacker)
			conn, _, err := h.Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	cli, _ := New(server.URL, "key")
	_, err := cli.GetQuotaSummary(context.Background(), "idx", "")
	var transportErr *TransportError
	if !errors.As(err, &transportErr) {
		t.Fatalf("transport outage masked by later account error: %v", err)
	}
}

func TestClientListAccountsTransportFailureIsReturned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer server.Close()
	cli, _ := New(server.URL, "")
	_, err := cli.ListAuthFiles(context.Background())
	var mgmt *ManagementAPIError
	if !errors.As(err, &mgmt) || mgmt.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected typed discovery failure, got %v", err)
	}
}

func TestManagementQuotaBadRequestIsTypedSeparatelyFromUpstreamBadRequest(t *testing.T) {
	for _, tc := range []struct {
		name  string
		outer bool
	}{
		{name: "outer management response", outer: true},
		{name: "upstream envelope"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.outer {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte("sensitive management response"))
					return
				}
				_, _ = w.Write([]byte(`{"status_code":400,"body":"sensitive upstream response"}`))
			}))
			defer server.Close()
			cli, _ := New(server.URL, "key")
			_, err := cli.GetCodexQuotaSummary(context.Background(), "idx", "")
			var accountErr *AccountQuotaError
			var queryErr *QuotaQueryError
			if tc.outer {
				if !errors.As(err, &accountErr) || accountErr.StatusCode != http.StatusBadRequest {
					t.Fatalf("outer 400 should be account-local typed error: %v", err)
				}
			} else if !errors.As(err, &queryErr) || errors.As(err, &accountErr) {
				t.Fatalf("upstream 400 should remain a quota query error: %v", err)
			}
			if strings.Contains(err.Error(), "sensitive") {
				t.Fatalf("response body leaked: %v", err)
			}
		})
	}
}

func TestQuotaQueryErrorPreservesUpstreamStatusWithoutBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status_code":429,"body":"sensitive response"}`))
	}))
	defer server.Close()
	cli, err := New(server.URL, "key")
	if err != nil {
		t.Fatal(err)
	}
	_, err = cli.GetCodexQuotaSummary(context.Background(), "idx", "")
	var queryErr *QuotaQueryError
	if !errors.As(err, &queryErr) || queryErr.StatusCode != 429 {
		t.Fatalf("expected structured status 429, got %#v", err)
	}
	if strings.Contains(err.Error(), "sensitive response") {
		t.Fatalf("error exposed response body: %v", err)
	}
}

func TestClientGetCodexQuotaSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/api-call" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var req APICallRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode api-call req: %v", err)
		}
		if req.URL != CodexQuotaEndpoint {
			t.Errorf("expected URL %s, got %s", CodexQuotaEndpoint, req.URL)
		}
		if req.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", req.Method)
		}
		if req.Header["OpenAI-Beta"] != "codex-1" {
			t.Errorf("missing or incorrect OpenAI-Beta header")
		}
		if req.Header["Chatgpt-Account-Id"] != "acc-123" {
			t.Errorf("expected Chatgpt-Account-Id 'acc-123', got %s", req.Header["Chatgpt-Account-Id"])
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status_code": 200,
			"body":        `{"rate_limit":{"primary_window":{"used_percent":15.0}}}`,
		})
	}))
	defer server.Close()

	cli, err := New(server.URL, "test-key")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	bytes, err := cli.GetCodexQuotaSummary(context.Background(), "idx-codex", "acc-123")
	if err != nil {
		t.Fatalf("GetCodexQuotaSummary error: %v", err)
	}
	if string(bytes) != `{"rate_limit":{"primary_window":{"used_percent":15.0}}}` {
		t.Errorf("unexpected response body: %s", string(bytes))
	}
}

func TestClientGetAuthFileDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"prefix":             "codex_1",
			"chatgpt_account_id": "acc-999",
		})
	}))
	defer server.Close()

	cli, err := New(server.URL, "test-key")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	meta, err := cli.GetAuthFileDetails(context.Background(), "codex.json")
	if err != nil {
		t.Fatalf("GetAuthFileDetails error: %v", err)
	}
	if meta.Prefix != "codex_1" || meta.ChatGPTAccountID != "acc-999" {
		t.Errorf("unexpected metadata: %+v", meta)
	}
}
