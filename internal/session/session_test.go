package session

import (
	"context"
	"errors"
	"net/http"
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
