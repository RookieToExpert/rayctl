package platform

import (
	"net/http"
	"time"
)

// D/PT large-page probes (2026-10-22, 30 requests) completed in <= 0.567s.
// Keep five seconds above the gateway's default 15s upstream timeout so that
// a gateway 504 can reach the retry policy, while keeping every attempt bounded.
const platformRequestTimeout = 20 * time.Second

func newPlatformHTTPClient() *http.Client {
	return &http.Client{Timeout: platformRequestTimeout}
}
