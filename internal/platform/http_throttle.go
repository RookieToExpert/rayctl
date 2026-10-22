package platform

import (
	"context"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// One shared budget per origin, across profiles and VC workers. The gateway
// policies do not publish a per-client QPS; six is a conservative client cap,
// not a claim that the server supports a particular request rate.
const platformMaxInFlight = 6

type platformEndpointBudget struct {
	slots     chan struct{}
	mu        sync.Mutex
	cooldown  time.Time
	next      time.Time
	slowUntil time.Time
}

type platformTransport struct {
	base  http.RoundTripper
	mu    sync.Mutex
	hosts map[string]*platformEndpointBudget
}

func newPlatformTransport(base http.RoundTripper) *platformTransport {
	return &platformTransport{base: base, hosts: map[string]*platformEndpointBudget{}}
}

func (t *platformTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	key := req.URL.Scheme + "://" + req.URL.Host
	t.mu.Lock()
	budget := t.hosts[key]
	if budget == nil {
		budget = &platformEndpointBudget{slots: make(chan struct{}, platformMaxInFlight)}
		t.hosts[key] = budget
	}
	t.mu.Unlock()
	select {
	case budget.slots <- struct{}{}:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
	release := func() { <-budget.slots }
	if err := budget.wait(req.Context()); err != nil {
		release()
		return nil, err
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		release()
		return resp, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		delay := retryAfterDelay(resp.Header.Get("Retry-After"), time.Now())
		if delay < time.Second {
			delay = time.Second
		}
		budget.mu.Lock()
		until := time.Now().Add(delay)
		if until.After(budget.cooldown) {
			budget.cooldown = until
		}
		budget.slowUntil = budget.cooldown.Add(5 * time.Second)
		budget.mu.Unlock()
	}
	if resp.Body == nil {
		release()
	} else {
		resp.Body = &budgetResponseBody{ReadCloser: resp.Body, release: release}
	}
	return resp, nil
}

type budgetResponseBody struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (b *budgetResponseBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.release)
	return err
}

func (b *platformEndpointBudget) wait(ctx context.Context) error {
	for {
		b.mu.Lock()
		now := time.Now()
		ready := b.cooldown
		if now.Before(b.slowUntil) && b.next.After(ready) {
			ready = b.next
		}
		if !now.Before(ready) {
			if now.Before(b.slowUntil) {
				b.next = now.Add(250 * time.Millisecond)
			}
			b.mu.Unlock()
			return nil
		}
		delay := time.Until(ready)
		b.mu.Unlock()
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func retryAfterDelay(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}

func platformRetryDelay(resp *http.Response, attempt int, now time.Time) (time.Duration, bool) {
	delay := time.Duration(100<<attempt) * time.Millisecond
	if resp != nil {
		hint := retryAfterDelay(resp.Header.Get("Retry-After"), now)
		if hint > 30*time.Second {
			return 0, false
		}
		if hint > delay {
			delay = hint
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			minimum := time.Second * time.Duration(1<<attempt)
			if delay < minimum {
				delay = minimum
			}
			// Positive jitter never retries earlier than Retry-After.
			delay += time.Duration(rand.Int64N(int64(delay/4 + 1)))
		}
	}
	return delay, true
}
