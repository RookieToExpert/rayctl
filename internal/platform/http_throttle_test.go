package platform

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPlatformTransportBoundsAllWorkersPerOrigin(t *testing.T) {
	var active, peak atomic.Int32
	transport := newPlatformTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		n := active.Add(1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		active.Add(-1)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header), Request: req}, nil
	}))
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodGet, "https://api.test/large-list", nil)
			resp, err := transport.RoundTrip(req)
			if err != nil {
				t.Error(err)
				return
			}
			resp.Body.Close()
		}()
	}
	wg.Wait()
	if peak.Load() > platformMaxInFlight || peak.Load() < 2 {
		t.Fatalf("peak=%d", peak.Load())
	}
}

func Test429CooldownIsSharedAndRecoveryIsStaggered(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	starts := []time.Time{}
	transport := newPlatformTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		status := 200
		if calls.Add(1) == 1 {
			status = 429
		} else {
			mu.Lock()
			starts = append(starts, time.Now())
			mu.Unlock()
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
	}))
	req, _ := http.NewRequest(http.MethodGet, "https://api.test/items", nil)
	first, _ := transport.RoundTrip(req)
	first.Body.Close()
	before := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			request, _ := http.NewRequest(http.MethodGet, "https://api.test/other-profile", nil)
			resp, err := transport.RoundTrip(request)
			if err != nil {
				t.Error(err)
				return
			}
			resp.Body.Close()
		}()
	}
	wg.Wait()
	if starts[0].Sub(before) < 950*time.Millisecond {
		t.Fatal("429 did not pause peers", starts)
	}
	for i := 1; i < len(starts); i++ {
		if starts[i].Sub(starts[i-1]) < 200*time.Millisecond {
			t.Fatal("recovery burst", starts)
		}
	}
}

func TestThrottleWaitHonorsCancellationAndKeepsBodySlot(t *testing.T) {
	transport := newPlatformTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
	}))
	bodies := []io.Closer{}
	for i := 0; i < platformMaxInFlight; i++ {
		req, _ := http.NewRequest(http.MethodGet, "https://api.test/items", nil)
		r, _ := transport.RoundTrip(req)
		bodies = append(bodies, r.Body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.test/items", nil)
	_, err := transport.RoundTrip(req)
	for _, body := range bodies {
		body.Close()
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestRetryAfterSecondsDateAndBounds(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	for _, value := range []string{"2", now.Add(2 * time.Second).UTC().Format(http.TimeFormat)} {
		resp := &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{value}}}
		delay, ok := platformRetryDelay(resp, 0, now)
		if !ok || delay < 2*time.Second || delay > 2500*time.Millisecond {
			t.Fatal(value, delay, ok)
		}
	}
	resp := &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"3600"}}}
	if _, ok := platformRetryDelay(resp, 0, now); ok {
		t.Fatal("unbounded retry wait")
	}
	for _, value := range []string{"bad", "-1", "0", ""} {
		if retryAfterDelay(value, now) != 0 {
			t.Fatal(value)
		}
	}
}
