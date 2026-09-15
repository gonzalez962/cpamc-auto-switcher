package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Default settings
const (
	DefaultProvider            = "antigravity"
	DefaultActivePrefix        = "agy"
	DefaultReservePrefixPrefix = "agy_"
	DefaultFiveHourThreshold   = 90.0
	DefaultWeeklyThreshold     = 95.0
)

// Config holds runtime configuration for cpamc-auto-switcher.
type Config struct {
	Endpoint            string  `json:"endpoint"`
	ManagementKey       string  `json:"management_key"`
	Provider            string  `json:"provider"`
	ActivePrefix        string  `json:"active_prefix"`
	ReservePrefixPrefix string  `json:"reserve_prefix_prefix"`
	FiveHourThreshold   float64 `json:"five_hour_threshold"`
	WeeklyThreshold     float64 `json:"weekly_threshold"`
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
	}
}

// Load reads and parses configuration from the given path. If path is empty,
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
	return nil
}
