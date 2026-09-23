package session

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoginCancelsPendingRequest(t *testing.T) {
	started := make(chan struct{})
	client := newLoginHTTPClient()
	if client.Timeout != 10*time.Second {
		t.Fatalf("login HTTP timeout = %s", client.Timeout)
	}
	client.Transport = loginRoundTripper(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := loginWithClient(ctx, client, "https://signin.example.test", "https://iam.example.test", "tester", "password", "")
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("login did not start its request")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Login() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("login request did not stop after cancellation")
	}
}

type loginRoundTripper func(*http.Request) (*http.Response, error)

func (fn loginRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func TestSaveReplacesSessionAtomicallyWithPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	store := &Store{Profiles: map[string]ProfileSession{"test": {AccessToken: "secret"}}}
	if err := Save(path, store); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) || after.Mode().Perm() != 0o600 {
		t.Fatalf("session was not atomically replaced with private permissions: %v", after.Mode())
	}
	loaded, err := Load(path)
	if err != nil || loaded.Profiles["test"].AccessToken != "secret" {
		t.Fatalf("Load() = %v, %v", loaded, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("session directory entries = %v, %v", entries, err)
	}
}

func TestAuthorizationCodeReportsBodyReadFailure(t *testing.T) {
	client := &http.Client{Transport: loginRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(failingReader{})}, nil
	})}
	_, _, err := exchangeAuthorizationCode(context.Background(), client, "https://example.test/token", "code", "redirect", "verifier", "state")
	if err == nil || !strings.Contains(err.Error(), "read authorization response") {
		t.Fatalf("exchangeAuthorizationCode() error = %v", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
