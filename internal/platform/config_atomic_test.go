package platform

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
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

func TestInvalidCurrentProfileFallsBackDeterministically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "platform.json")
	content := `{"current_profile":"bad","profiles":{"bad":{},"z":{"access_key":"ak","secret_key":"sk","subscription_id":"sub"},"a":{"access_key":"ak","secret_key":"sk","subscription_id":"sub"}}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	warnings := capturePlatformStderr(t, func() {
		for range 20 {
			client, err := newVirtualClusterClientFromFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := client.CurrentProfileName(); got != "a" {
				t.Fatalf("CurrentProfileName() = %q, want a", got)
			}
		}
	})
	if count := strings.Count(warnings, `falling back to "a"`); count != 20 {
		t.Fatalf("fallback warnings = %d, want 20: %q", count, warnings)
	}
}

func TestResponseBodyExcerptLimitsLargeErrors(t *testing.T) {
	got := responseBodyExcerpt([]byte(strings.Repeat("x", 1000)))
	if len(got) != 515 || !strings.HasSuffix(got, "...") {
		t.Fatalf("excerpt length = %d, suffix = %q", len(got), got[len(got)-3:])
	}
}

func TestResponseBodyExcerptPreservesUTF8Boundary(t *testing.T) {
	body := []byte(strings.Repeat("a", 511) + "错误" + strings.Repeat("b", 20))
	got := responseBodyExcerpt(body)
	if got != strings.Repeat("a", 511)+"..." || !utf8.ValidString(got) {
		t.Fatalf("excerpt has invalid UTF-8 boundary: %q", got)
	}
}

func TestResponseBodyExcerptChineseLimit(t *testing.T) {
	got := responseBodyExcerpt([]byte(strings.Repeat("错", 400)))
	if !utf8.ValidString(got) || !strings.HasSuffix(got, "...") || len(got) > 515 {
		t.Fatalf("invalid Chinese excerpt: %q (%d bytes)", got, len(got))
	}
}

func TestMissingCurrentProfileWarnsButExplicitProfileDoesNot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "platform.json")
	content := `{"profiles":{"z":{"access_key":"ak","secret_key":"sk","subscription_id":"sub"},"a":{"access_key":"ak","secret_key":"sk","subscription_id":"sub"}}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	warning := capturePlatformStderr(t, func() {
		client, err := newVirtualClusterClientFromFile(path)
		if err != nil || client.CurrentProfileName() != "a" {
			t.Fatalf("fallback client = %v, err = %v", client, err)
		}
	})
	if warning != "Warning: current_profile \"\" not found in platform.json; falling back to \"a\"\n" {
		t.Fatalf("missing-profile warning = %q", warning)
	}
	if err := os.WriteFile(path, []byte(`{"current_profile":"z","profiles":{"z":{"access_key":"ak","secret_key":"sk","subscription_id":"sub"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	warning = capturePlatformStderr(t, func() {
		if _, err := newVirtualClusterClientFromFile(path); err != nil {
			t.Fatal(err)
		}
	})
	if warning != "" {
		t.Fatalf("explicit-profile warning = %q", warning)
	}
}

func capturePlatformStderr(t *testing.T, call func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stderr
	os.Stderr = writer
	defer func() {
		os.Stderr = previous
		writer.Close()
		reader.Close()
	}()
	type captured struct {
		output []byte
		err    error
	}
	done := make(chan captured, 1)
	go func() {
		output, err := io.ReadAll(reader)
		done <- captured{output, err}
	}()
	call()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	return string(result.output)
}

func TestDefaultProfileUsesSortedName(t *testing.T) {
	profiles := map[string]config{"ailabdev": {}, "aardvark": {}}
	if got := normalizeCurrentProfileName("", profiles); got != "aardvark" {
		t.Fatalf("default profile = %q, want aardvark", got)
	}
}

func TestOrderedProfilesDoesNotRereadMetricsEnvironment(t *testing.T) {
	client := &VirtualClusterClient{metricsBaseURL: "https://initial.example", metricsUsername: "initial"}
	t.Setenv("RAYCTL_METRICS_BASE_URL", "https://changed.example")
	t.Setenv("RAYCTL_METRICS_USERNAME", "changed")
	profiles := client.orderedProfiles()
	if len(profiles) != 1 || profiles[0].MetricsBaseURL != "https://initial.example" || profiles[0].MetricsUsername != "initial" {
		t.Fatalf("ordered profiles = %#v", profiles)
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
