package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// State tracks runtime metadata across executions, such as the last quota check timestamp.
type ProblemAccount struct {
	Provider string `json:"provider"`
	Identity string `json:"identity"`
	Reason   string `json:"reason"`
	Status   string `json:"status,omitempty"`
}

type State struct {
	LastCheck      time.Time         `json:"last_check"`
	Problems       []ProblemAccount  `json:"problem_accounts,omitempty"`
	ActiveAccounts map[string]string `json:"active_accounts,omitempty"`
	mu             sync.Mutex        `json:"-"`
}

func activeKey(provider, profile string) string {
	return strings.ToLower(strings.TrimSpace(provider)) + "\x00" + profile
}

func (s *State) Active(provider, profile string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ActiveAccounts[activeKey(provider, profile)]
}

func (s *State) hasProblemLocked(provider, identity string) bool {
	for _, p := range s.Problems {
		if strings.EqualFold(p.Provider, provider) && p.Identity == identity {
			return true
		}
	}
	return false
}

func (s *State) SyncActives(provider string, active map[string]string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ActiveAccounts == nil {
		s.ActiveAccounts = make(map[string]string)
	}
	prefix := strings.ToLower(strings.TrimSpace(provider)) + "\x00"
	for key := range s.ActiveAccounts {
		if strings.HasPrefix(key, prefix) {
			delete(s.ActiveAccounts, key)
		}
	}
	for profile, identity := range active {
		if strings.TrimSpace(identity) != "" && !s.hasProblemLocked(provider, identity) {
			s.ActiveAccounts[activeKey(provider, profile)] = identity
		}
	}
}

func (s *State) SetActive(provider, profile, identity string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ActiveAccounts == nil {
		s.ActiveAccounts = make(map[string]string)
	}
	key := activeKey(provider, profile)
	if strings.TrimSpace(identity) == "" || s.hasProblemLocked(provider, identity) {
		delete(s.ActiveAccounts, key)
		return
	}
	s.ActiveAccounts[key] = identity
}

func (s *State) ProblemSnapshot() []ProblemAccount {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ProblemAccount(nil), s.Problems...)
}

func (s *State) HasProblem(provider, identity string) bool {
	for _, p := range s.ProblemSnapshot() {
		if strings.EqualFold(p.Provider, provider) && p.Identity == identity {
			return true
		}
	}
	return false
}

func (s *State) RecordProblem(provider, identity, reason string) {
	if s == nil || strings.TrimSpace(identity) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeActiveIdentityLocked(provider, identity)
	for i := range s.Problems {
		if strings.EqualFold(s.Problems[i].Provider, provider) && s.Problems[i].Identity == identity {
			s.Problems[i].Reason = reason
			s.Problems[i].Status = "pending"
			return
		}
	}
	s.Problems = append(s.Problems, ProblemAccount{Provider: provider, Identity: identity, Reason: reason, Status: "pending"})
}

func (s *State) removeActiveIdentityLocked(provider, identity string) {
	prefix := strings.ToLower(strings.TrimSpace(provider)) + "\x00"
	for key, active := range s.ActiveAccounts {
		if strings.HasPrefix(key, prefix) && active == identity {
			delete(s.ActiveAccounts, key)
		}
	}
}

func (s *State) MarkProblem(provider, identity, reason, status string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeActiveIdentityLocked(provider, identity)
	for i := range s.Problems {
		if strings.EqualFold(s.Problems[i].Provider, provider) && s.Problems[i].Identity == identity {
			s.Problems[i].Reason = reason
			s.Problems[i].Status = status
			return
		}
	}
}

func (s *State) ResolveProblem(provider, identity string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < len(s.Problems); {
		if strings.EqualFold(s.Problems[i].Provider, provider) && s.Problems[i].Identity == identity {
			s.Problems = append(s.Problems[:i], s.Problems[i+1:]...)
		} else {
			i++
		}
	}
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

	for key, identity := range s.ActiveAccounts {
		provider := strings.SplitN(key, "\x00", 2)[0]
		if s.hasProblemLocked(provider, identity) {
			delete(s.ActiveAccounts, key)
		}
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

	s.mu.Lock()
	data, err := json.MarshalIndent(s, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write state file %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure state file permissions: %w", err)
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
