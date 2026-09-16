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
