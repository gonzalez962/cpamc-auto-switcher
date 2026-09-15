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
