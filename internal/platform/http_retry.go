package platform

import (
	"context"
	"errors"
	"net/http"
	"time"
)

func doPlatformRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return client.Do(req)
	}
	for attempt := 0; ; attempt++ {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if attempt >= 2 || !retryableGETResult(resp, err) {
			return resp, err
		}
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		pause := time.NewTimer(time.Duration(100<<attempt) * time.Millisecond)
		select {
		case <-req.Context().Done():
			pause.Stop()
			return nil, req.Context().Err()
		case <-pause.C:
		}
	}
}

func retryableGETResult(resp *http.Response, err error) bool {
	if err != nil {
		return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
	}
	if resp == nil {
		return false
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable:
		return true
	default:
		return false
	}
}
