package podevidence

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

type imageTestTransport func(*http.Request) (*http.Response, error)

func (f imageTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHarborTransportFailureSkipsOtherImagesUntilTTLExpires(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	calls := 0
	http.DefaultTransport = imageTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, fmt.Errorf("connect: network unreachable")
	})
	for _, ref := range []string{"registry.test/p/a:v1", "registry.test/p/b:v1"} {
		if _, err := lookupImage(context.Background(), nil, corev1.Pod{}, Image{Ref: ref}); err == nil {
			t.Fatal("expected unavailable")
		}
	}
	if calls != 1 {
		t.Fatalf("unreachable registry queried %d times", calls)
	}
	old := time.Now().Add(-2 * time.Minute)
	path := registryFailurePath("registry.test")
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	lookupImage(context.Background(), nil, corev1.Pod{}, Image{Ref: "registry.test/p/c:v1"})
	if calls != 2 {
		t.Fatal("TTL did not allow recovery", calls)
	}
}

func TestHarborAuthFailureIsNotRememberedAsNetworkFailure(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	calls := 0
	http.DefaultTransport = imageTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("unauthorized")), Header: make(http.Header), Request: r}, nil
	})
	for i := 0; i < 2; i++ {
		lookupImage(context.Background(), nil, corev1.Pod{}, Image{Ref: "registry.test/p/image:v1"})
	}
	if calls != 2 {
		t.Fatal("authentication was cached as network failure")
	}
}

func TestHarborTimeoutIsShortAndRemembered(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	calls := 0
	http.DefaultTransport = imageTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	started := time.Now()
	_, err := lookupImage(context.Background(), nil, corev1.Pod{}, Image{Ref: "registry.test/p/image:v1"})
	elapsed := time.Since(started)
	if err == nil || elapsed > 3500*time.Millisecond || elapsed < 2500*time.Millisecond {
		t.Fatal(elapsed, err)
	}
	lookupImage(context.Background(), nil, corev1.Pod{}, Image{Ref: "registry.test/p/other:v1"})
	if calls != 1 {
		t.Fatal("timeout not cached")
	}
}
