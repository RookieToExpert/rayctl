package platform

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestHTTPStatusErrorPreservesStatusAndExistingText(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusForbidden} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			const body = `{"message":"upstream previously returned 404: unavailable"}`
			const endpoint = "https://api.test/items"
			client := &VirtualClusterClient{httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				response := jsonHTTPResponse(r, body)
				response.StatusCode = code
				return response, nil
			})}}
			_, err := client.doRequestWithProfile(context.Background(), clientProfile{AccessKey: "ak", SecretKey: "sk"}, http.MethodGet, endpoint, "", nil)
			var status *HTTPStatusError
			if !errors.As(fmt.Errorf("wrapped: %w", err), &status) || status.StatusCode != code || status.URL != endpoint {
				t.Fatalf("status lost: %v", err)
			}
			if want := fmt.Sprintf("request %s returned %d: %s", endpoint, code, body); err.Error() != want {
				t.Fatalf("error text changed: %q, want %q", err, want)
			}
		})
	}
}
