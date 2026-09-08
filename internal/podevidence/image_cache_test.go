package podevidence

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func TestRegistryFailureCacheSkipsRequestsAndExpires(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path := registryFailurePath("registry.test")
	rememberRegistryFailure(path)
	_, err := lookupImage(context.Background(), nil, corev1.Pod{}, Image{Ref: "registry.test/project/image:v1"})
	if err == nil || !strings.Contains(err.Error(), "recently unreachable") {
		t.Fatalf("expected cached failure: %v", err)
	}
	t.Setenv("HTTPS_PROXY", "http://different-proxy:3128")
	if registryFailurePath("registry.test") == path {
		t.Fatal("proxy changes must invalidate cache")
	}
	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) < time.Minute {
		t.Fatal("cache did not expire")
	}
}
