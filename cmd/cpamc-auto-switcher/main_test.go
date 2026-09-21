package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cpamc-auto-switcher/internal/client"
	"cpamc-auto-switcher/internal/config"
	"cpamc-auto-switcher/internal/state"
	"cpamc-auto-switcher/internal/switcher"
)

func TestCooldownIntegration(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	statePath := filepath.Join(tempDir, "state.json")

	apiHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiHits++
		switch r.URL.Path {
		case "/v0/management/auth-files":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"id": "acc-1", "name": "acc-1.json", "auth_index": "idx-1", "provider": "antigravity"},
				},
			})
		case "/v0/management/auth-files/download":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"prefix": "agy",
			})
		case "/v0/management/api-call":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"groups": [{
					"displayName": "Models",
					"buckets": [
						{"displayName": "Weekly Limit", "remainingFraction": 0.50}
					]
				}]
			}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		Endpoint:            server.URL,
		ManagementKey:       "dummy-key",
		Provider:            "antigravity",
		ActivePrefix:        "agy",
		ReservePrefixPrefix: "agy_",
		FiveHourThreshold:   90.0,
		WeeklyThreshold:     95.0,
		CooldownMinutes:     5.0,
	}
	if _, err := cfg.Save(configPath); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	cli, err := client.New(cfg.Endpoint, cfg.ManagementKey)
	if err != nil {
		t.Fatalf("failed to init client: %v", err)
	}
	sw := switcher.New(cfg, cli)

	// Step 1: Initial state (file does not exist) -> ShouldCheck returns true
	appState, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	cooldown := time.Duration(cfg.CooldownMinutes * float64(time.Minute))
	if !appState.ShouldCheck(cooldown, time.Now()) {
		t.Fatal("expected initial check to be allowed")
	}

	// Simulate first check
	res, err := sw.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("sw.Run failed: %v", err)
	}
	if res.Rotated {
		t.Errorf("expected no rotation, got %v", res.Rotated)
	}

	// Save state with now
	appState.LastCheck = time.Now().UTC()
	if err := appState.Save(statePath); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	firstHits := apiHits
	if firstHits == 0 {
		t.Fatal("expected API calls on first check")
	}

	// Step 2: Immediate second check (within cooldown, not forced) -> ShouldCheck returns false
	reloadedState, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("failed to reload state: %v", err)
	}

	if reloadedState.ShouldCheck(cooldown, time.Now()) {
		t.Fatal("expected ShouldCheck to be false within 5-minute cooldown")
	}

	// Verify that if skipped, apiHits does not increase
	if apiHits != firstHits {
		t.Fatalf("expected apiHits to remain %d, got %d", firstHits, apiHits)
	}

	// Step 3: Fast-forward time (past 5 minutes) -> ShouldCheck returns true
	futureTime := time.Now().Add(6 * time.Minute)
	if !reloadedState.ShouldCheck(cooldown, futureTime) {
		t.Fatal("expected ShouldCheck to be true 6 minutes later")
	}

	// Step 4: Verify state file exists on disk and is valid JSON
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}
	var checkMap map[string]string
	if err := json.Unmarshal(data, &checkMap); err != nil {
		t.Fatalf("invalid json in state file: %v", err)
	}
	if _, ok := checkMap["last_check"]; !ok {
		t.Fatal("expected 'last_check' key in state file JSON")
	}
}
func TestExtractAccountEmail(t *testing.T) {
	tests := []struct {
		id       string
		name     string
		email    string
		expected string
	}{
		{
			id:       "antigravity-userone@example.com.json",
			name:     "antigravity-userone@example.com.json",
			email:    "",
			expected: "userone@example.com",
		},
		{
			id:       "codex-5c0fd0b4-usertwo@example.org-plus.json",
			name:     "codex-5c0fd0b4-usertwo@example.org-plus.json",
			email:    "",
			expected: "usertwo@example.org",
		},
		{
			id:       "codex-6c0ft0b8-userthree@example.net-plus.json",
			name:     "codex-6c0ft0b8-userthree@example.net-plus.json",
			email:    "",
			expected: "userthree@example.net",
		},
		{
			id:       "some-custom-account",
			name:     "some-name",
			email:    "direct@example.org",
			expected: "direct@example.org",
		},
		{
			id:       "no-email-account-id",
			name:     "no-email-name",
			email:    "",
			expected: "no-email-account-id",
		},
	}

	for _, tt := range tests {
		got := extractAccountEmail(tt.id, tt.name, tt.email)
		if got != tt.expected {
			t.Errorf("extractAccountEmail(%q, %q, %q) = %q, expected %q",
				tt.id, tt.name, tt.email, got, tt.expected)
		}
	}
}

func TestAbbreviateEmail(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "requested example testerone@example.com",
			input:    "testerone@example.com",
			expected: "tes...one@example.com",
		},
		{
			name:     "local part exactly 6 characters unchanged",
			input:    "direct@example.org",
			expected: "direct@example.org",
		},
		{
			name:     "local part exactly 6 characters alphanumeric",
			input:    "123456@example.com",
			expected: "123456@example.com",
		},
		{
			name:     "local part 5 characters unchanged",
			input:    "admin@example.com",
			expected: "admin@example.com",
		},
		{
			name:     "local part 1 character unchanged",
			input:    "a@example.com",
			expected: "a@example.com",
		},
		{
			name:     "local part exactly 7 characters abbreviated",
			input:    "1234567@example.com",
			expected: "123...567@example.com",
		},
		{
			name:     "local part 8 characters abbreviated",
			input:    "12345678@example.com",
			expected: "123...678@example.com",
		},
		{
			name:     "longer email userlongname12345",
			input:    "userlongname12345@example.com",
			expected: "use...345@example.com",
		},
		{
			name:     "longer email elevenchars",
			input:    "elevenchars@example.net",
			expected: "ele...ars@example.net",
		},
		{
			name:     "longer email with subdomain",
			input:    "developer@sub.example.net",
			expected: "dev...per@sub.example.net",
		},
		{
			name:     "non-email identifier unchanged",
			input:    "no-email-account-id",
			expected: "no-email-account-id",
		},
		{
			name:     "empty string unchanged",
			input:    "",
			expected: "",
		},
		{
			name:     "empty local part unchanged",
			input:    "@example.com",
			expected: "@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := abbreviateEmail(tt.input)
			if got != tt.expected {
				t.Errorf("abbreviateEmail(%q) = %q, expected %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestAccountDisplayAbbreviationEndToEnd(t *testing.T) {
	id := "antigravity-testerone@example.com.json"
	raw := extractAccountEmail(id, id, "")
	if raw != "testerone@example.com" {
		t.Fatalf("extractAccountEmail = %q, expected %q", raw, "testerone@example.com")
	}

	display := abbreviateEmail(raw)
	expected := "tes...one@example.com"
	if display != expected {
		t.Fatalf("abbreviateEmail(%q) = %q, expected %q", raw, display, expected)
	}
}

func TestProfileListingAndFiltering(t *testing.T) {
	prefixes := map[string]string{
		"acc-def.json": "agy",
		"acc-res.json": "agy_1",
		"acc-p1.json":  "agy_p1",
		"acc-p2.json":  "agy_p1_1",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"id": "acc-def", "name": "acc-def.json", "auth_index": "1", "provider": "antigravity"},
					{"id": "acc-res", "name": "acc-res.json", "auth_index": "2", "provider": "antigravity"},
					{"id": "acc-p1", "name": "acc-p1.json", "auth_index": "3", "provider": "antigravity"},
					{"id": "acc-p2", "name": "acc-p2.json", "auth_index": "4", "provider": "antigravity"},
				},
			})
		case "/v0/management/auth-files/download":
			name := r.URL.Query().Get("name")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"prefix": prefixes[name]})
		case "/v0/management/api-call":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"groups":[{"buckets":[{"displayName":"5-Hour Limit","remainingFraction":0.80}]}]}`))
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		Endpoint:      server.URL,
		ManagementKey: "dummy",
		Provider:      "antigravity",
	}
	cli, _ := client.New(server.URL, "dummy")
	sw := switcher.New(cfg, cli)

	accounts, err := sw.ListAccounts(context.Background())
	if err != nil {
		t.Fatalf("ListAccounts failed: %v", err)
	}
	if len(accounts) != 4 {
		t.Fatalf("expected 4 accounts, got %d", len(accounts))
	}

	// Filter by profile "p1"
	var p1Accounts []switcher.AccountState
	for _, a := range accounts {
		if a.Profile == "p1" {
			p1Accounts = append(p1Accounts, a)
		}
	}
	if len(p1Accounts) != 2 {
		t.Fatalf("expected 2 accounts in p1, got %d", len(p1Accounts))
	}
	if !p1Accounts[0].IsActive || p1Accounts[0].Prefix != "agy_p1" {
		t.Errorf("expected active p1 first, got %+v", p1Accounts[0])
	}
	if !p1Accounts[1].IsReserve || p1Accounts[1].Prefix != "agy_p1_1" {
		t.Errorf("expected reserve p1 second, got %+v", p1Accounts[1])
	}

	// Filter by profile "default" ("")
	var defAccounts []switcher.AccountState
	for _, a := range accounts {
		if a.Profile == "" {
			defAccounts = append(defAccounts, a)
		}
	}
	if len(defAccounts) != 2 {
		t.Fatalf("expected 2 default accounts, got %d", len(defAccounts))
	}
	if !defAccounts[0].IsActive || defAccounts[0].Prefix != "agy" {
		t.Errorf("expected active default first, got %+v", defAccounts[0])
	}
}

func TestProfileSwitchAndRotationIntegration(t *testing.T) {
	prefixes := map[string]string{
		"def.json":  "agy",
		"def1.json": "agy_1",
		"p1a.json":  "agy_p1",
		"p1r.json":  "agy_p1_1",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"id": "def", "name": "def.json", "auth_index": "1", "provider": "antigravity"},
					{"id": "def1", "name": "def1.json", "auth_index": "2", "provider": "antigravity"},
					{"id": "p1a", "name": "p1a.json", "auth_index": "3", "provider": "antigravity"},
					{"id": "p1r", "name": "p1r.json", "auth_index": "4", "provider": "antigravity"},
				},
			})
		case "/v0/management/auth-files/download":
			name := r.URL.Query().Get("name")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"prefix": prefixes[name]})
		case "/v0/management/auth-files/fields":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			prefixes[req["name"]] = req["prefix"]
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/v0/management/api-call":
			var req client.APICallRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			w.Header().Set("Content-Type", "application/json")
			switch req.AuthIndex {
			case "1", "2", "4":
				_, _ = w.Write([]byte(`{"groups":[{"buckets":[{"displayName":"5-Hour Limit","remainingFraction":0.90}]}]}`))
			case "3":
				// p1 active exceeds threshold (95% used)
				_, _ = w.Write([]byte(`{"groups":[{"buckets":[{"displayName":"5-Hour Limit","remainingFraction":0.05}]}]}`))
			}
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		Endpoint:          server.URL,
		ManagementKey:     "dummy",
		Provider:          "antigravity",
		FiveHourThreshold: 90.0,
		WeeklyThreshold:   95.0,
	}
	cli, _ := client.New(server.URL, "dummy")
	sw := switcher.New(cfg, cli)

	// 1. Manual switch within p1
	manRes, err := sw.SwitchToAccount(context.Background(), "agy_p1_1", false)
	if err != nil {
		t.Fatalf("manual switch failed: %v", err)
	}
	if !manRes.Rotated {
		t.Fatalf("expected manual switch rotation")
	}
	if prefixes["p1r.json"] != "agy_p1" || prefixes["p1a.json"] != "agy_p1_1" {
		t.Fatalf("unexpected prefixes after manual switch: %+v", prefixes)
	}

	// Reset prefixes for automatic rotation test
	prefixes["p1a.json"] = "agy_p1"
	prefixes["p1r.json"] = "agy_p1_1"

	// 2. Automatic rotation filtering by profile "p1"
	rotRes, err := sw.Run(context.Background(), false, "p1")
	if err != nil {
		t.Fatalf("sw.Run failed: %v", err)
	}
	if !rotRes.Rotated {
		t.Fatalf("expected rotation for p1: %s", rotRes.Reason)
	}
	if prefixes["p1r.json"] != "agy_p1" || prefixes["p1a.json"] != "agy_p1_1" {
		t.Fatalf("unexpected prefixes after auto rotation: %+v", prefixes)
	}
	// Verify default pool was untouched
	if prefixes["def.json"] != "agy" || prefixes["def1.json"] != "agy_1" {
		t.Fatalf("default pool modified: %+v", prefixes)
	}
}

