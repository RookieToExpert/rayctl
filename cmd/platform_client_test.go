package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequirePlatformClientReportsInvalidProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "platform.json")
	if err := os.WriteFile(path, []byte(`{"profiles":{"d":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAYCTL_PLATFORM_CONFIG", path)
	_, err := requirePlatformClient()
	if err == nil || !strings.Contains(err.Error(), "no usable profiles") {
		t.Fatalf("requirePlatformClient() error = %v", err)
	}
}

func TestOptionalPlatformClientReportsInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "platform.json")
	if err := os.WriteFile(path, []byte(`{"profiles":`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAYCTL_PLATFORM_CONFIG", path)
	_, err := optionalPlatformClient()
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("optionalPlatformClient() error = %v", err)
	}
}
