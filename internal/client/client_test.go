package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
			"body": `{"rate_limit":{"primary_window":{"used_percent":15.0}}}`,
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
