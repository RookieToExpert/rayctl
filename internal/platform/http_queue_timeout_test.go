package platform

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientTimeoutIncludesAdmissionWait(t *testing.T) {
	var calls atomic.Int32
	transport := newPlatformTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
	}))
	var bodies []io.Closer
	for i := 0; i < platformMaxInFlight; i++ {
		req, _ := http.NewRequest("GET", "https://queue.test/items", nil)
		resp, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, resp.Body)
	}
	defer func() {
		for _, body := range bodies {
			body.Close()
		}
	}()
	client := &http.Client{Timeout: 20 * time.Millisecond, Transport: transport}
	req, _ := http.NewRequest("GET", "https://queue.test/items", nil)
	_, err := client.Do(req)
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != platformMaxInFlight {
		t.Fatalf("backend calls=%d err=%v", calls.Load(), err)
	}
}

func TestPlatformAdmissionDoesNotConsumeNetworkBudget(t *testing.T) {
	for _, waitKind := range []string{"slot", "cooldown"} {
		t.Run(waitKind, func(t *testing.T) {
			var sent atomic.Int32
			transport := newPlatformTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				sent.Add(1)
				deadline, ok := req.Context().Deadline()
				if !ok || time.Until(deadline) < 15*time.Millisecond {
					t.Error("queue wait consumed network budget")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
			}))
			transport.requestTimeout = 30 * time.Millisecond
			transport.queueTimeout = 300 * time.Millisecond
			budget := &platformEndpointBudget{slots: make(chan struct{}, platformMaxInFlight)}
			transport.hosts["https://separate.test"] = budget
			if waitKind == "cooldown" {
				budget.cooldown = time.Now().Add(60 * time.Millisecond)
			} else {
				for i := 0; i < platformMaxInFlight; i++ {
					budget.slots <- struct{}{}
				}
				time.AfterFunc(60*time.Millisecond, func() { <-budget.slots })
			}
			client := &http.Client{Transport: transport}
			req, _ := http.NewRequest("GET", "https://separate.test/items", nil)
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if sent.Load() != 1 {
				t.Fatal(sent.Load())
			}
		})
	}
}

func TestPlatformAdmissionTimeoutRemainsBounded(t *testing.T) {
	transport := newPlatformTransport(roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("sent after queue timeout"); return nil, nil }))
	transport.queueTimeout = 15 * time.Millisecond
	budget := &platformEndpointBudget{slots: make(chan struct{}, platformMaxInFlight)}
	for i := 0; i < platformMaxInFlight; i++ {
		budget.slots <- struct{}{}
	}
	transport.hosts["https://separate.test"] = budget
	req, _ := http.NewRequest("GET", "https://separate.test/items", nil)
	_, err := transport.RoundTrip(req)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "并发槽位") {
		t.Fatal(err)
	}
}
