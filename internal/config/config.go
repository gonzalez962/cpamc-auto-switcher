package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Default settings
const (
	DefaultEndpoint            = "http://localhost:8317"
	DefaultProvider            = "all"
	DefaultActivePrefix        = "agy"
	DefaultReservePrefixPrefix = "agy_"
	DefaultFiveHourThreshold   = 90.0
	DefaultWeeklyThreshold     = 95.0
	DefaultCooldownMinutes     = 5.0
)

// SupportedProviders lists providers handled by cpamc-auto-switcher.
var SupportedProviders = []string{"antigravity", "codex"}

// Config holds runtime configuration for cpamc-auto-switcher.
type Config struct {
	Endpoint            string  `json:"endpoint"`
	ManagementKey       string  `json:"management_key"`
	Provider            string  `json:"provider"`
	ActivePrefix        string  `json:"active_prefix"`
	ReservePrefixPrefix string  `json:"reserve_prefix_prefix"`
	FiveHourThreshold   float64 `json:"five_hour_threshold"`
	WeeklyThreshold     float64 `json:"weekly_threshold"`
	CooldownMinutes     float64 `json:"cooldown_minutes,omitempty"`
}

// DefaultConfigPath returns ~/.local/share/cpamc-auto-switcher/config.json.
func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "cpamc-auto-switcher", "config.json"), nil
}

// NewDefaultConfig returns a configuration pre-filled with sensible defaults.
func NewDefaultConfig() *Config {
	return &Config{
		Provider:            DefaultProvider,
		ActivePrefix:        DefaultActivePrefix,
		ReservePrefixPrefix: DefaultReservePrefixPrefix,
		FiveHourThreshold:   DefaultFiveHourThreshold,
		WeeklyThreshold:     DefaultWeeklyThreshold,
		CooldownMinutes:     DefaultCooldownMinutes,
	}
}

// ResolvedProviders returns the list of providers to inspect. If Provider is "all",
// it returns all supported providers.
func (c *Config) ResolvedProviders() []string {
	p := strings.ToLower(strings.TrimSpace(c.Provider))
	if p == "" || p == "all" {
		return []string{"antigravity", "codex"}
	}
	return []string{p}
}

// ConventionForProvider resolves active and reserve prefixes for the specified provider.
func (c *Config) ConventionForProvider(provider string) (activePrefix, reservePrefixPrefix string) {
	p := strings.ToLower(strings.TrimSpace(provider))
	switch p {
	case "codex":
		if strings.EqualFold(c.Provider, "codex") && c.ActivePrefix != "" && c.ActivePrefix != "agy" {
			return c.ActivePrefix, c.ReservePrefixPrefix
		}
		return "codex", "codex_"
	case "antigravity":
		if (strings.EqualFold(c.Provider, "antigravity") || c.Provider == "all") && c.ActivePrefix != "" && c.ReservePrefixPrefix != "" {
			return c.ActivePrefix, c.ReservePrefixPrefix
		}
		return "agy", "agy_"
	default:
		if c.ActivePrefix != "" && c.ReservePrefixPrefix != "" {
			return c.ActivePrefix, c.ReservePrefixPrefix
		}
		return "agy", "agy_"
	}
}

// ParsedPrefix contains the structured decomposition of an account prefix.
type ParsedPrefix struct {
	BasePrefix   string
	Profile      string
	IsActive     bool
	IsReserve    bool
	ReserveIndex int
	Matched      bool
}

// ParsePrefix parses an account prefix given the base active prefix for the provider.
// E.g. for baseActive "agy":
// - "agy" -> default pool active (Profile: "", IsActive: true)
// - "agy_1" -> default pool reserve 1 (Profile: "", IsReserve: true, ReserveIndex: 1)
// - "agy_p1" -> profile p1 active (Profile: "p1", IsActive: true)
// - "agy_p1_1" -> profile p1 reserve 1 (Profile: "p1", IsReserve: true, ReserveIndex: 1)
// - "agy_team_a" -> profile team_a active (Profile: "team_a", IsActive: true)
// - "agy_team_a_1" -> profile team_a reserve 1 (Profile: "team_a", IsReserve: true, ReserveIndex: 1)
func ParsePrefix(prefix, baseActive string) ParsedPrefix {
	if baseActive == "" || prefix == "" {
		return ParsedPrefix{}
	}

	if prefix == baseActive {
		return ParsedPrefix{
			BasePrefix:   baseActive,
			Profile:      "",
			IsActive:     true,
			IsReserve:    false,
			ReserveIndex: 0,
			Matched:      true,
		}
	}

	prefixWithUnder := baseActive + "_"
	if !strings.HasPrefix(prefix, prefixWithUnder) {
		return ParsedPrefix{}
	}

	remainder := strings.TrimPrefix(prefix, prefixWithUnder)
	if remainder == "" || strings.HasPrefix(remainder, "_") || strings.HasSuffix(remainder, "_") {
		return ParsedPrefix{}
	}

	isDigits := func(s string) bool {
		if len(s) == 0 {
			return false
		}
		for _, r := range s {
			if r < '0' || r > '9' {
				return false
			}
		}
		return true
	}

	lastIdx := strings.LastIndex(remainder, "_")
	if lastIdx != -1 {
		tail := remainder[lastIdx+1:]
		if isDigits(tail) {
			idx, err := strconv.Atoi(tail)
			if err == nil && idx >= 0 {
				profilePart := remainder[:lastIdx]
				if profilePart != "" && !strings.HasSuffix(profilePart, "_") {
					return ParsedPrefix{
						BasePrefix:   baseActive,
						Profile:      profilePart,
						IsActive:     false,
						IsReserve:    true,
						ReserveIndex: idx,
						Matched:      true,
					}
				}
			}
		}
		// If tail is not pure digits, the remainder itself is a profile with underscores (e.g. team_a)
		return ParsedPrefix{
			BasePrefix:   baseActive,
			Profile:      remainder,
			IsActive:     true,
			IsReserve:    false,
			ReserveIndex: 0,
			Matched:      true,
		}
	}

	// No underscores in remainder
	if isDigits(remainder) {
		idx, err := strconv.Atoi(remainder)
		if err == nil && idx >= 0 {
			return ParsedPrefix{
				BasePrefix:   baseActive,
				Profile:      "",
				IsActive:     false,
				IsReserve:    true,
				ReserveIndex: idx,
				Matched:      true,
			}
		}
	}

	return ParsedPrefix{
		BasePrefix:   baseActive,
		Profile:      remainder,
		IsActive:     true,
		IsReserve:    false,
		ReserveIndex: 0,
		Matched:      true,
	}
}

// ConventionForProfile resolves active and reserve prefixes for the specified provider and profile.
func (c *Config) ConventionForProfile(provider, profile string) (activePrefix, reservePrefixPrefix string) {
	baseActive, baseReserve := c.ConventionForProvider(provider)
	trimmedProfile := strings.TrimSpace(profile)
	if trimmedProfile == "" {
		return baseActive, baseReserve
	}
	active := fmt.Sprintf("%s_%s", baseActive, trimmedProfile)
	return active, active + "_"
}

// DefaultConfigPath is used.
func Load(customPath string) (*Config, string, error) {
	path := customPath
	if strings.TrimSpace(path) == "" {
		defaultPath, err := DefaultConfigPath()
		if err != nil {
			return nil, "", err
		}
		path = defaultPath
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, path, err
	}

	cfg := NewDefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, path, fmt.Errorf("parse config json: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, path, err
	}

	return cfg, path, nil
}

// Save writes configuration to disk ensuring directory 0700 and file 0600 permissions.
func (c *Config) Save(customPath string) (string, error) {
	if c == nil {
		return "", errors.New("cannot save nil config")
	}

	path := customPath
	if strings.TrimSpace(path) == "" {
		defaultPath, err := DefaultConfigPath()
		if err != nil {
			return "", err
		}
		path = defaultPath
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create config directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return "", fmt.Errorf("write config file %s: %w", path, err)
	}

	return path, nil
}

// Validate checks sanity of config attributes.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.Endpoint) == "" {
		return errors.New("endpoint cannot be empty")
	}
	if strings.TrimSpace(c.ManagementKey) == "" {
		return errors.New("management_key cannot be empty")
	}
	if strings.TrimSpace(c.Provider) == "" {
		c.Provider = DefaultProvider
	}
	if strings.TrimSpace(c.ActivePrefix) == "" {
		c.ActivePrefix = DefaultActivePrefix
	}
	if strings.TrimSpace(c.ReservePrefixPrefix) == "" {
		c.ReservePrefixPrefix = DefaultReservePrefixPrefix
	}
	if c.FiveHourThreshold <= 0 || c.FiveHourThreshold > 100 {
		c.FiveHourThreshold = DefaultFiveHourThreshold
	}
	if c.WeeklyThreshold <= 0 || c.WeeklyThreshold > 100 {
		c.WeeklyThreshold = DefaultWeeklyThreshold
	}
	if c.CooldownMinutes <= 0 {
		c.CooldownMinutes = DefaultCooldownMinutes
	}
	return nil
}
