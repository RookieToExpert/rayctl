package platform

import (
	"path/filepath"
	"testing"
)

func TestDefaultConfigPathUsesCurrentUserHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("RAYCTL_PLATFORM_CONFIG", "")

	want := filepath.Join(home, ".rayctl", "platform.json")
	if got := DefaultConfigPath(); got != want {
		t.Fatalf("DefaultConfigPath() = %q, want %q", got, want)
	}
}

func TestDefaultConfigPathSupportsOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "custom-platform.json")
	t.Setenv("RAYCTL_PLATFORM_CONFIG", override)

	if got := DefaultConfigPath(); got != override {
		t.Fatalf("DefaultConfigPath() = %q, want %q", got, override)
	}
}
