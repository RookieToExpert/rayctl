package platform

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGetJSONAuthVariants(t *testing.T) {
	profile := clientProfile{AccessKey: "ak", SecretKey: "sk", CMSAccessKey: "cms-ak", CMSSecretKey: "cms-sk"}
	tests := []struct {
		name string
		want string
		call func(*VirtualClusterClient, *bool) error
	}{
		{"profile", `accesskey="ak"`, func(c *VirtualClusterClient, out *bool) error {
			return c.getJSONWithProfile(context.Background(), profile, "https://example.test/data", out)
		}},
		{"cms", `accesskey="cms-ak"`, func(c *VirtualClusterClient, out *bool) error {
			return c.getJSONWithCMSProfile(context.Background(), profile, "https://example.test/data", out)
		}},
		{"cms token", "Bearer token", func(c *VirtualClusterClient, out *bool) error {
			return c.getJSONWithCMSProfileToken(context.Background(), profile, "https://example.test/data", "token", out)
		}},
		{"bearer", "Bearer token", func(c *VirtualClusterClient, out *bool) error {
			return c.getJSONWithBearerProfile(context.Background(), profile, "https://example.test/data", "token", out)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &VirtualClusterClient{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if got := req.Header.Get("Authorization"); !strings.Contains(got, tt.want) {
					t.Errorf("Authorization = %q, want %q", got, tt.want)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("true")), Header: make(http.Header), Request: req}, nil
			})}}
			var value bool
			if err := tt.call(client, &value); err != nil || !value {
				t.Fatalf("get JSON = %v, %v", value, err)
			}
		})
	}
}
