package switcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"cpamc-auto-switcher/internal/client"
	"cpamc-auto-switcher/internal/config"
)

func TestSwitcherWorkflow(t *testing.T) {
	var mu sync.Mutex
	prefixes := map[string]string{
		"acc-1.json": "agy",   // active
		"acc-2.json": "agy_1", // reserve 1
		"acc-3.json": "agy_2", // reserve 2
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch r.URL.Path {
		case "/v0/management/auth-files":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"id": "acc-1", "name": "acc-1.json", "auth_index": "idx-1", "provider": "antigravity"},
					{"id": "acc-2", "name": "acc-2.json", "auth_index": "idx-2", "provider": "antigravity"},
					{"id": "acc-3", "name": "acc-3.json", "auth_index": "idx-3", "provider": "antigravity"},
				},
			})
		case "/v0/management/auth-files/download":
			name := r.URL.Query().Get("name")
			p := prefixes[name]
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"prefix": p,
			})
		case "/v0/management/api-call":
			var req client.APICallRequest
			_ = json.NewDecoder(r.Body).Decode(&req)

			w.Header().Set("Content-Type", "application/json")
			switch req.AuthIndex {
			case "idx-1":
				// Active account: 5h limit reached 92% (threshold 90%) -> Trigger rotation
				_, _ = w.Write([]byte(`{
					"groups": [{
						"displayName": "Models",
						"buckets": [
							{"displayName": "5-Hour Limit", "remainingFraction": 0.08},
							{"displayName": "Weekly Limit", "remainingFraction": 0.20}
						]
					}]
				}`))
			case "idx-2":
				// Reserve 1: 5h remaining 60%, weekly remaining 70% (min available = 60%)
				_, _ = w.Write([]byte(`{
					"groups": [{
						"displayName": "Models",
						"buckets": [
							{"displayName": "5-Hour Limit", "remainingFraction": 0.60},
							{"displayName": "Weekly Limit", "remainingFraction": 0.70}
						]
					}]
				}`))
			case "idx-3":
				// Reserve 2: 5h remaining 80%, weekly remaining 85% (min available = 80%) -> BETTER CANDIDATE
				_, _ = w.Write([]byte(`{
					"groups": [{
						"displayName": "Models",
						"buckets": [
							{"displayName": "5-Hour Limit", "remainingFraction": 0.80},
							{"displayName": "Weekly Limit", "remainingFraction": 0.85}
						]
					}]
				}`))
			}
		case "/v0/management/auth-files/fields":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			name := req["name"]
			prefixes[name] = req["prefix"]
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		Endpoint:            server.URL,
		ManagementKey:       "dummy",
		Provider:            "antigravity",
		ActivePrefix:        "agy",
		ReservePrefixPrefix: "agy_",
		FiveHourThreshold:   90.0,
		WeeklyThreshold:     95.0,
	}

	cli, err := client.New(server.URL, "dummy")
	if err != nil {
		t.Fatalf("client.New failed: %v", err)
	}

	sw := New(cfg, cli)

	// Test ListAccounts
	accounts, err := sw.ListAccounts(context.Background())
	if err != nil {
		t.Fatalf("ListAccounts failed: %v", err)
	}
	if len(accounts) != 3 {
		t.Fatalf("expected 3 accounts, got %d", len(accounts))
	}
	if !accounts[0].IsActive || accounts[0].Entry.ID != "acc-1" {
		t.Errorf("expected acc-1 to be active and first in list, got %+v", accounts[0])
	}

	// Test Manual SwitchToAccount by prefix
	manualRes, err := sw.SwitchToAccount(context.Background(), "agy_1", false)
	if err != nil {
		t.Fatalf("SwitchToAccount failed: %v", err)
	}
	if !manualRes.Rotated {
		t.Errorf("expected manual switch rotation to be true, got %s", manualRes.Reason)
	}
	if prefixes["acc-2.json"] != "agy" || prefixes["acc-1.json"] != "agy_1" {
		t.Errorf("prefixes after manual switch unexpected: %+v", prefixes)
	}

	// Revert prefixes for automatic flow test
	prefixes["acc-1.json"] = "agy"
	prefixes["acc-2.json"] = "agy_1"
	prefixes["acc-3.json"] = "agy_2"

	// 1. Dry run test
	dryRes, err := sw.Run(context.Background(), true)
	if err != nil {
		t.Fatalf("dry run failed: %v", err)
	}
	if !dryRes.Rotated {
		t.Fatalf("expected rotation in dry run, got reason: %s", dryRes.Reason)
	}
	if dryRes.SelectedReserve != "acc-3" {
		t.Errorf("expected acc-3 to be selected (highest quota), got %s", dryRes.SelectedReserve)
	}

	// Confirm prefixes were not changed in dry run
	if prefixes["acc-1.json"] != "agy" || prefixes["acc-3.json"] != "agy_2" {
		t.Fatalf("prefixes modified during dry run: %+v", prefixes)
	}

	// 2. Real execution test
	res, err := sw.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("real run failed: %v", err)
	}
	if !res.Rotated {
		t.Fatalf("expected real run rotation, got reason: %s", res.Reason)
	}

	// Verify direct swap occurred (acc-3 became agy, acc-1 became agy_2)
	mu.Lock()
	defer mu.Unlock()
	if prefixes["acc-3.json"] != "agy" {
		t.Errorf("acc-3.json expected prefix 'agy', got %q", prefixes["acc-3.json"])
	}
	if prefixes["acc-1.json"] != "agy_2" {
		t.Errorf("acc-1.json expected prefix 'agy_2', got %q", prefixes["acc-1.json"])
	}
}

func TestCodexSwitcherWorkflow(t *testing.T) {
	var mu sync.Mutex
	prefixes := map[string]string{
		"codex-1.json": "codex",   // active
		"codex-2.json": "codex_1", // reserve
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch r.URL.Path {
		case "/v0/management/auth-files":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"id": "codex-1", "name": "codex-1.json", "auth_index": "idx-c1", "provider": "codex"},
					{"id": "codex-2", "name": "codex-2.json", "auth_index": "idx-c2", "provider": "codex"},
				},
			})
		case "/v0/management/auth-files/download":
			name := r.URL.Query().Get("name")
			p := prefixes[name]
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"prefix":             p,
				"chatgpt_account_id": "acc-" + name,
			})
		case "/v0/management/api-call":
			var req client.APICallRequest
			_ = json.NewDecoder(r.Body).Decode(&req)

			w.Header().Set("Content-Type", "application/json")
			switch req.AuthIndex {
			case "idx-c1":
				// Active account 5h limit used 93% -> Triggers rotation
				_, _ = w.Write([]byte(`{
					"rate_limit": {
						"allowed": true,
						"primary_window": {
							"used_percent": 93.0
						},
						"secondary_window": {
							"used_percent": 30.0
						}
					}
				}`))
			case "idx-c2":
				// Reserve account 5h limit used 10%
				_, _ = w.Write([]byte(`{
					"rate_limit": {
						"allowed": true,
						"primary_window": {
							"used_percent": 10.0
						},
						"secondary_window": {
							"used_percent": 20.0
						}
					}
				}`))
			}
		case "/v0/management/auth-files/fields":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			name := req["name"]
			prefixes[name] = req["prefix"]
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		Endpoint:            server.URL,
		ManagementKey:       "dummy",
		Provider:            "codex",
		FiveHourThreshold:   90.0,
		WeeklyThreshold:     95.0,
	}

	cli, err := client.New(cfg.Endpoint, cfg.ManagementKey)
	if err != nil {
		t.Fatalf("client.New failed: %v", err)
	}
	sw := New(cfg, cli)

	// Test ListAccounts
	accounts, err := sw.ListAccounts(context.Background())
	if err != nil {
		t.Fatalf("ListAccounts failed: %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accounts))
	}
	if !accounts[0].IsActive || accounts[0].Prefix != "codex" {
		t.Errorf("expected first account to be active with prefix 'codex', got %+v", accounts[0])
	}

	// Automatic rotation run
	res, err := sw.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !res.Rotated {
		t.Fatalf("expected rotation, got: %s", res.Reason)
	}

	mu.Lock()
	if prefixes["codex-2.json"] != "codex" {
		t.Errorf("expected codex-2.json to become active 'codex', got %q", prefixes["codex-2.json"])
	}
	if prefixes["codex-1.json"] != "codex_1" {
		t.Errorf("expected codex-1.json to be demoted to 'codex_1', got %q", prefixes["codex-1.json"])
	}
	mu.Unlock()

	// Test Manual switch back
	manualRes, err := sw.SwitchToAccount(context.Background(), "codex-1.json", false)
	if err != nil {
		t.Fatalf("SwitchToAccount failed: %v", err)
	}
	if !manualRes.Rotated {
		t.Fatalf("expected manual rotation true, got: %s", manualRes.Reason)
	}
	mu.Lock()
	if prefixes["codex-1.json"] != "codex" || prefixes["codex-2.json"] != "codex_1" {
		t.Errorf("unexpected prefixes after manual switch: %+v", prefixes)
	}
	mu.Unlock()
}
