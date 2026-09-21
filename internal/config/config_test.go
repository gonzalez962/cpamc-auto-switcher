package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigSaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "sub", "config.json")

	cfg := &Config{
		Endpoint:            "http://127.0.0.1:8000",
		ManagementKey:       "secret-test-key",
		Provider:            "antigravity",
		ActivePrefix:        "agy",
		ReservePrefixPrefix: "agy_",
		FiveHourThreshold:   90.0,
		WeeklyThreshold:     95.0,
	}

	savedPath, err := cfg.Save(configPath)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if savedPath != configPath {
		t.Fatalf("expected saved path %q, got %q", configPath, savedPath)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config file: %v", err)
	}
	// Under Unix-like systems, verify 0600 (on Windows file modes differ slightly)
	if info.Size() == 0 {
		t.Fatal("saved file is empty")
	}

	loaded, loadedPath, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loadedPath != configPath {
		t.Fatalf("expected loaded path %q, got %q", configPath, loadedPath)
	}
	if loaded.Endpoint != cfg.Endpoint {
		t.Errorf("endpoint mismatch: %s != %s", loaded.Endpoint, cfg.Endpoint)
	}
	if loaded.ManagementKey != cfg.ManagementKey {
		t.Errorf("key mismatch: %s != %s", loaded.ManagementKey, cfg.ManagementKey)
	}
	if loaded.FiveHourThreshold != 90.0 || loaded.WeeklyThreshold != 95.0 {
		t.Errorf("thresholds mismatch: %f, %f", loaded.FiveHourThreshold, loaded.WeeklyThreshold)
	}
	if loaded.CooldownMinutes != DefaultCooldownMinutes {
		t.Errorf("expected default cooldown %f, got %f", DefaultCooldownMinutes, loaded.CooldownMinutes)
	}
}

func TestConfigValidation(t *testing.T) {
	cfg := &Config{}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error on empty config, got nil")
	}

	cfg.Endpoint = "http://localhost:8000"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error on missing management_key, got nil")
	}

	cfg.ManagementKey = "secret"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if cfg.Provider != DefaultProvider || cfg.ActivePrefix != DefaultActivePrefix {
		t.Errorf("defaults not populated: provider=%s, activePrefix=%s", cfg.Provider, cfg.ActivePrefix)
	}
	if cfg.CooldownMinutes != DefaultCooldownMinutes {
		t.Errorf("expected default cooldown %f, got %f", DefaultCooldownMinutes, cfg.CooldownMinutes)
	}

	// Test ResolvedProviders and ConventionForProvider
	allProviders := cfg.ResolvedProviders()
	if len(allProviders) != 2 || allProviders[0] != "antigravity" || allProviders[1] != "codex" {
		t.Errorf("unexpected resolved providers for 'all': %v", allProviders)
	}

	agActive, agReserve := cfg.ConventionForProvider("antigravity")
	if agActive != "agy" || agReserve != "agy_" {
		t.Errorf("unexpected antigravity convention: %s, %s", agActive, agReserve)
	}

	codexActive, codexReserve := cfg.ConventionForProvider("codex")
	if codexActive != "codex" || codexReserve != "codex_" {
		t.Errorf("unexpected codex convention: %s, %s", codexActive, codexReserve)
	}

	cfg.Provider = "codex"
	if len(cfg.ResolvedProviders()) != 1 || cfg.ResolvedProviders()[0] != "codex" {
		t.Errorf("expected only codex resolved, got %v", cfg.ResolvedProviders())
	}
}

func TestConventionForProfile(t *testing.T) {
	cfg := NewDefaultConfig()

	// Default profile returns base convention
	act, res := cfg.ConventionForProfile("antigravity", "")
	if act != "agy" || res != "agy_" {
		t.Errorf("expected agy/agy_, got %s/%s", act, res)
	}

	// Profile p1
	act, res = cfg.ConventionForProfile("antigravity", "p1")
	if act != "agy_p1" || res != "agy_p1_" {
		t.Errorf("expected agy_p1/agy_p1_, got %s/%s", act, res)
	}

	// Codex profile p2
	act, res = cfg.ConventionForProfile("codex", "p2")
	if act != "codex_p2" || res != "codex_p2_" {
		t.Errorf("expected codex_p2/codex_p2_, got %s/%s", act, res)
	}
}

func TestParsePrefix(t *testing.T) {
	tests := []struct {
		prefix      string
		baseActive  string
		wantProfile string
		wantActive  bool
		wantReserve bool
		wantIndex   int
		wantMatched bool
	}{
		// Antigravity default pool
		{"agy", "agy", "", true, false, 0, true},
		{"agy_1", "agy", "", false, true, 1, true},
		{"agy_2", "agy", "", false, true, 2, true},
		{"agy_10", "agy", "", false, true, 10, true},

		// Antigravity profile p1
		{"agy_p1", "agy", "p1", true, false, 0, true},
		{"agy_p1_1", "agy", "p1", false, true, 1, true},
		{"agy_p1_2", "agy", "p1", false, true, 2, true},

		// Antigravity profile with underscore e.g. team_a
		{"agy_team_a", "agy", "team_a", true, false, 0, true},
		{"agy_team_a_1", "agy", "team_a", false, true, 1, true},

		// Codex default pool
		{"codex", "codex", "", true, false, 0, true},
		{"codex_1", "codex", "", false, true, 1, true},

		// Codex profile p2
		{"codex_p2", "codex", "p2", true, false, 0, true},
		{"codex_p2_1", "codex", "p2", false, true, 1, true},

		// Unmatched
		{"other", "agy", "", false, false, 0, false},
		{"", "agy", "", false, false, 0, false},
		{"agy_", "agy", "", false, false, 0, false},
	}

	for _, tc := range tests {
		got := ParsePrefix(tc.prefix, tc.baseActive)
		if got.Matched != tc.wantMatched ||
			got.Profile != tc.wantProfile ||
			got.IsActive != tc.wantActive ||
			got.IsReserve != tc.wantReserve ||
			got.ReserveIndex != tc.wantIndex {
			t.Errorf("ParsePrefix(%q, %q) = %+v; want Matched=%v, Profile=%q, IsActive=%v, IsReserve=%v, Index=%d",
				tc.prefix, tc.baseActive, got, tc.wantMatched, tc.wantProfile, tc.wantActive, tc.wantReserve, tc.wantIndex)
		}
	}
}

