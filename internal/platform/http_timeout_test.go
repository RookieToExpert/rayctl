package platform

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestPlatformRequestTimeoutLeavesGatewayHeadroom(t *testing.T) {
	client := newPlatformHTTPClient()
	transport := client.Transport.(*platformTransport)
	if client.Timeout != 0 || transport.requestTimeout != 20*time.Second || transport.requestTimeout <= 15*time.Second || transport.queueTimeout <= 0 {
		t.Fatalf("client=%v network=%v queue=%v", client.Timeout, transport.requestTimeout, transport.queueTimeout)
	}
}

func TestPlatformClientTimeoutIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	client := newPlatformHTTPClient()
	client.Timeout = 20 * time.Millisecond
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.test/large-list", nil)
	_, err := doPlatformRequest(client, req)
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
}
