package version

import (
	"testing"
)

func TestVersionNotEmpty(t *testing.T) {
	if Version == "" {
		t.Fatal("expected non-empty Version")
	}
	if Version != "0.1.0" {
		t.Errorf("expected version 0.1.0, got %q", Version)
	}
}
