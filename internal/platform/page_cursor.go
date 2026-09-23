package platform

import "strings"

type pageCursor struct {
	seen map[string]struct{}
}

func newPageCursor(initial string) *pageCursor {
	return &pageCursor{seen: map[string]struct{}{strings.TrimSpace(initial): {}}}
}

func (cursor *pageCursor) advance(next string) bool {
	next = strings.TrimSpace(next)
	if next == "" || len(cursor.seen) >= 10000 {
		return false
	}
	if _, exists := cursor.seen[next]; exists {
		return false
	}
	cursor.seen[next] = struct{}{}
	return true
}
