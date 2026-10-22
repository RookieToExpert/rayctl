package platform

import "fmt"

// HTTPStatusError retains the response status without changing the diagnostic
// text used by existing callers.
type HTTPStatusError struct {
	URL        string
	StatusCode int
	Body       string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("request %s returned %d: %s", e.URL, e.StatusCode, e.Body)
}
