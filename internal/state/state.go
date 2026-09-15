package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// State tracks runtime metadata across executions, such as the last quota check timestamp.
type State struct {
	LastCheck time.Time `json:"last_check"`
}

// DefaultStatePath returns the state file path given the loaded config file path.
// If configPath is empty, it resolves ~/.local/share/cpamc-auto-switcher/state.json.
func DefaultStatePath(configPath string) (string, error) {
	if strings.TrimSpace(configPath) != "" {
		return filepath.Join(filepath.Dir(configPath), "state.json"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "cpamc-auto-switcher", "state.json"), nil
}

// Load reads and parses state from the specified path.
// If the file does not exist, it returns an empty State and no error.
func Load(path string) (*State, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("state path cannot be empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{}, nil
		}
		return &State{}, fmt.Errorf("read state file: %w", err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return &State{}, fmt.Errorf("parse state json: %w", err)
	}

	return &s, nil
}

// Save writes state to disk ensuring directory 0700 and file 0600 permissions.
func (s *State) Save(path string) error {
	if s == nil {
		return errors.New("cannot save nil state")
	}
	if strings.TrimSpace(path) == "" {
		return errors.New("state path cannot be empty")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write state file %s: %w", path, err)
	}

	return nil
}

// ShouldCheck returns true if enough time has passed since LastCheck, or if LastCheck is zero/uninitialized,
// or if cooldown is zero or negative. Clock skew backwards safely returns true.
func (s *State) ShouldCheck(cooldown time.Duration, now time.Time) bool {
	if s == nil || s.LastCheck.IsZero() || cooldown <= 0 {
		return true
	}

	// Clock skew protection: if now is before LastCheck, allow check
	if now.Before(s.LastCheck) {
		return true
	}

	return now.Sub(s.LastCheck) >= cooldown
}
