package service

import "fmt"

// ResolveListLimit preserves explicit limits when scanning a time window.
// Window scans without an explicit limit return up to 5000 records.
func ResolveListLimit(limit int, windowed, limitChanged bool) (int, error) {
	if windowed {
		if !limitChanged {
			return 5000, nil
		}
		if limit < 1 {
			return 0, fmt.Errorf("--limit must be at least 1")
		}
		return min(limit, 5000), nil
	}
	if limit == 0 && !limitChanged {
		return 50, nil
	}
	if limit < 1 || limit > 1000 {
		return 0, fmt.Errorf("--limit must be between 1 and 1000")
	}
	return limit, nil
}
