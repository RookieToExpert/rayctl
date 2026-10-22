package platform

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGETGatewayTimeoutRetriesButPoolOverflowDoesNot(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		header   http.Header
		body     string
		attempts int
	}{
		{"gateway timeout", 504, nil, "upstream request timeout", 3},
		{"pool header", 503, http.Header{"X-Envoy-Overloaded": []string{"true"}}, "busy", 1},
		{"overflow flag", 503, http.Header{"X-Envoy-Response-Flags": []string{"UO"}}, "busy", 1},
		{"overflow body", 503, nil, "upstream connect error or disconnect/reset before headers. reset reason: overflow", 1},
		{"overflow detail", 504, http.Header{"X-Envoy-Response-Code-Details": []string{"upstream_reset_before_response_started{connection_pool_overflow}"}}, "busy", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, closed := 0, 0
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tc.status, Header: tc.header, Body: &countedRetryBody{Reader: strings.NewReader(tc.body), closed: &closed}, Request: req}, nil
			})}
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.test/items", nil)
			resp, err := doPlatformRequest(client, req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if calls != tc.attempts || closed != calls || err != nil || string(body) != tc.body {
				t.Fatalf("calls=%d closed=%d body=%q err=%v", calls, closed, body, err)
			}
		})
	}
}

type countedRetryBody struct {
	io.Reader
	closed *int
}

func (b *countedRetryBody) Close() error { *b.closed++; return nil }

func TestGETDoesNotRetryCancellationTimeoutOrLocalExhaustion(t *testing.T) {
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded, errors.New("connection pool exhausted"), errors.New("dial: too many open files")} {
		calls := 0
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, failure })}
		req, _ := http.NewRequest(http.MethodGet, "https://api.test/items", nil)
		_, err := doPlatformRequest(client, req)
		if err == nil || calls != 1 {
			t.Fatalf("%v: calls=%d err=%v", failure, calls, err)
		}
	}
}
