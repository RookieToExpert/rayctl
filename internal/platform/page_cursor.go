package platform

import (
	"fmt"
	"os"
	"strings"
)

const maxPageCursorTokens = 10000

type pageCursor struct {
	seen map[string]struct{}
}

func newPageCursor(initial string) *pageCursor {
	return &pageCursor{seen: map[string]struct{}{strings.TrimSpace(initial): {}}}
}

func (cursor *pageCursor) advance(next string) bool {
	next = strings.TrimSpace(next)
	if next == "" {
		return false
	}
	if _, exists := cursor.seen[next]; exists {
		return false
	}
	if len(cursor.seen) >= maxPageCursorTokens {
		fmt.Fprintf(os.Stderr, "Warning: pagination stopped after %d page tokens; results may be incomplete\n", maxPageCursorTokens)
		return false
	}
	cursor.seen[next] = struct{}{}
	return true
}
