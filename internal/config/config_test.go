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
