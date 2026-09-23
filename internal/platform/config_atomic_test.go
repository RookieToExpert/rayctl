package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestInvalidPlatformConfigReportsPathAndParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "platform.json")
	if err := os.WriteFile(path, []byte(`{"profiles":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfigFile(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("ValidateConfigFile() = %v", err)
	}
	t.Setenv("RAYCTL_PLATFORM_CONFIG", path)
	if _, err := NewVirtualClusterClientFromEnvWithError(); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("NewVirtualClusterClientFromEnvWithError() = %v", err)
	}
}

func TestConcurrentConfigUpdatesPreserveProfilesAndPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".rayctl")
	path := filepath.Join(dir, "platform.json")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfigSnapshot(path, &ConfigSnapshot{
		CurrentProfile: "base",
		Profiles:       map[string]ConfigProfile{"base": {AccessKey: "ak", SecretKey: "sk", Subscription: "sub"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results <- UpdateConfigSnapshot(path, func(cfg *ConfigSnapshot) error {
				cfg.Profiles[fmt.Sprintf("profile-%d", index)] = ConfigProfile{AccessKey: "ak", SecretKey: "sk", Subscription: "sub"}
				return nil
			})
		}(i)
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}

	loaded, err := LoadConfigSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Profiles) != 3 {
		t.Fatalf("profiles = %d, want 3", len(loaded.Profiles))
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("config permissions = %o", got)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("config directory permissions = %o", got)
	}
}
