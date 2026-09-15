package state

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStateLoadNonExistent(t *testing.T) {
	tempDir := t.TempDir()
	nonExistentPath := filepath.Join(tempDir, "state.json")

	s, err := Load(nonExistentPath)
	if err != nil {
		t.Fatalf("expected no error loading non-existent state, got: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil state")
	}
	if !s.LastCheck.IsZero() {
		t.Fatalf("expected zero LastCheck time, got: %v", s.LastCheck)
	}
}

func TestStateSaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")

	now := time.Now().Truncate(time.Second).UTC()
	original := &State{
		LastCheck: now,
	}

	if err := original.Save(statePath); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	loaded, err := Load(statePath)
	if err != nil {
		t.Fatalf("failed to load saved state: %v", err)
	}

	if !loaded.LastCheck.Equal(now) {
		t.Fatalf("expected LastCheck %v, got %v", now, loaded.LastCheck)
	}
}

func TestShouldCheck(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	cooldown := 5 * time.Minute

	// Case 1: Uninitialized LastCheck
	emptyState := &State{}
	if !emptyState.ShouldCheck(cooldown, now) {
		t.Errorf("expected ShouldCheck=true for uninitialized state")
	}

	// Case 2: Nil state
	var nilState *State
	if !nilState.ShouldCheck(cooldown, now) {
		t.Errorf("expected ShouldCheck=true for nil state")
	}

	// Case 3: Zero or negative cooldown
	sWithTime := &State{LastCheck: now.Add(-1 * time.Minute)}
	if !sWithTime.ShouldCheck(0, now) {
		t.Errorf("expected ShouldCheck=true when cooldown is 0")
	}
	if !sWithTime.ShouldCheck(-1*time.Minute, now) {
		t.Errorf("expected ShouldCheck=true when cooldown is negative")
	}

	// Case 4: Within cooldown (e.g. 2 minutes since last check, cooldown 5m)
	withinCooldown := &State{LastCheck: now.Add(-2 * time.Minute)}
	if withinCooldown.ShouldCheck(cooldown, now) {
		t.Errorf("expected ShouldCheck=false within cooldown period")
	}

	// Case 5: Exactly at cooldown (5 minutes since last check)
	atCooldown := &State{LastCheck: now.Add(-5 * time.Minute)}
	if !atCooldown.ShouldCheck(cooldown, now) {
		t.Errorf("expected ShouldCheck=true at exact cooldown duration")
	}

	// Case 6: Past cooldown (10 minutes since last check)
	pastCooldown := &State{LastCheck: now.Add(-10 * time.Minute)}
	if !pastCooldown.ShouldCheck(cooldown, now) {
		t.Errorf("expected ShouldCheck=true when past cooldown")
	}

	// Case 7: Clock skew backwards (now is before LastCheck)
	skewed := &State{LastCheck: now.Add(5 * time.Minute)}
	if !skewed.ShouldCheck(cooldown, now) {
		t.Errorf("expected ShouldCheck=true on clock skew backwards")
	}
}

func TestDefaultStatePath(t *testing.T) {
	cfgPath := filepath.Join("some", "dir", "config.json")
	p, err := DefaultStatePath(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join("some", "dir", "state.json")
	if p != expected {
		t.Errorf("expected %s, got %s", expected, p)
	}

	defaultP, err := DefaultStatePath("")
	if err != nil {
		t.Fatalf("unexpected error resolving default state path: %v", err)
	}
	if filepath.Base(defaultP) != "state.json" {
		t.Errorf("expected file name state.json, got %s", defaultP)
	}
}
