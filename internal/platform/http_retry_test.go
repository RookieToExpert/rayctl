package platform

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPlatformGETRetriesTransientFailures(t *testing.T) {
	attempts := 0
	client := &VirtualClusterClient{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				return nil, errors.New("connection reset by peer")
			}
			status := http.StatusOK
			body := `{"ok":true}`
			if attempts == 2 {
				status = http.StatusServiceUnavailable
				body = "unavailable"
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		})},
	}
	var payload struct {
		OK bool `json:"ok"`
	}
	err := client.getJSONWithProfile(context.Background(), clientProfile{AccessKey: "ak", SecretKey: "sk"}, "https://api.example.test/items", &payload)
	if err != nil || !payload.OK || attempts != 3 {
		t.Fatalf("GET attempts = %d, payload = %#v, err = %v", attempts, payload, err)
	}
}

func TestPlatformPOSTDoesNotRetry(t *testing.T) {
	attempts := 0
	client := &VirtualClusterClient{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("unavailable")), Request: req}, nil
		})},
	}
	_, err := client.doRequestWithProfile(context.Background(), clientProfile{AccessKey: "ak", SecretKey: "sk"}, http.MethodPost, "https://api.example.test/items", "application/json", []byte(`{}`))
	if err == nil || attempts != 1 {
		t.Fatalf("POST attempts = %d, err = %v", attempts, err)
	}
}
