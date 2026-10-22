package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

var errUnsupportedIAMUserFilter = errors.New("IAM user filter is unsupported")

const iamBindingPolicyRetryPageSize = 1000

type iamPage[T any] struct {
	items []T
	next  string
	total int
}

// IAM tokens are page numbers on deployed D/PT servers. Use a fixed page size,
// deduplicate resource/binding identities, and validate totals. A single fresh
// scan can recover a concurrent insert/delete; persistent drift is an error.
func collectIAMPages[T any](ctx context.Context, pageSize, limit int, numberedFallback bool, fetch func(string, int) (iamPage[T], error)) ([]T, error) {
	return collectIAMPagesWithRetrySize(ctx, pageSize, pageSize, limit, numberedFallback, fetch)
}

// Binding policies sorted by creation time can overlap at equal timestamps.
// A larger, bounded rescan changes those boundaries. Restart at page 1 and keep
// the size fixed within each scan, since deployed tokens are page numbers.
func collectIAMPagesWithRetrySize[T any](ctx context.Context, pageSize, retryPageSize, limit int, numberedFallback bool, fetch func(string, int) (iamPage[T], error)) ([]T, error) {
	for pass := 0; pass < 2; pass++ {
		if pass > 0 && retryPageSize > pageSize {
			pageSize = retryPageSize
		}
		if limit > 0 && limit < pageSize {
			pageSize = limit
		}
		items := []T{}
		seen := map[string]struct{}{}
		token := "1"
		cursor := newPageCursor(token)
		expected := -1
		unstable := false
		duplicates := false
		for {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			page, err := fetch(token, pageSize)
			if err != nil {
				return nil, err
			}
			if page.total > 0 {
				if expected >= 0 && expected != page.total {
					unstable = true
				}
				expected = page.total
			}
			before := len(items)
			pageSeen := map[string]struct{}{}
			for _, item := range page.items {
				key := iamItemIdentity(item)
				if _, ok := seen[key]; ok {
					if _, samePage := pageSeen[key]; !samePage {
						duplicates = true
					}
					continue
				}
				pageSeen[key] = struct{}{}
				seen[key] = struct{}{}
				items = append(items, item)
			}
			if len(page.items) > 0 && len(items) == before {
				unstable = true
				break
			}
			if limit > 0 && len(items) >= limit {
				break
			}
			if expected >= 0 && len(items) >= expected {
				break
			}
			if len(page.items) == 0 {
				break
			}
			next := strings.TrimSpace(page.next)
			if next == "" || next == token {
				if numberedFallback && len(page.items) >= pageSize {
					n, err := strconv.Atoi(token)
					if err != nil {
						unstable = true
						break
					}
					next = strconv.Itoa(n + 1)
				} else {
					if next == token {
						return nil, fmt.Errorf("IAM 分页游标重复 %q；结果可能不完整", next)
					}
					break
				}
			}
			if !cursor.advance(next) {
				return nil, fmt.Errorf("IAM 分页游标重复或达到页数上限 %q；结果可能不完整", next)
			}
			token = next
		}
		want := expected
		if limit > 0 && (want < 0 || limit < want) {
			want = limit
		}
		if expected >= 0 && len(items) < want {
			unstable = true
		}
		if duplicates && expected < 0 {
			unstable = true
		}
		if !unstable {
			if limit > 0 && len(items) > limit {
				items = items[:limit]
			}
			return items, nil
		}
		if pass == 1 {
			return nil, fmt.Errorf("IAM 分页结果不稳定，重新读取后仍无法校验完整性（去重后 %d 条，total_size=%d）；请稍后重试", len(items), expected)
		}
		fmt.Fprintln(os.Stderr, "Warning: IAM 分页存在重复或计数不一致，正在从首页重新读取以校验完整性")
	}
	panic("unreachable")
}

func iamItemIdentity(item any) string {
	switch value := item.(type) {
	case IAMUser:
		if value.ID != "" {
			return value.ID
		}
		if value.Username != "" {
			return value.Username
		}
	case IAMGroup:
		if value.ID != "" {
			return value.ID
		}
		if value.Name != "" {
			return value.TenantID + "/" + value.Name
		}
	case IAMRoleInfo:
		if value.ID != "" {
			return value.ID
		}
		if value.RoleName != "" {
			return value.RoleName
		}
	case IAMBindingPolicy:
		// One policy ID can bind several members. Keep those distinct.
		identify := ""
		if value.ID == "" || value.MemberValue == "" {
			identify = strings.TrimSpace(value.MemberIdentify)
		}
		return strings.Join([]string{strings.TrimSpace(value.ID), strings.TrimSpace(value.Scope), strings.ToUpper(strings.TrimSpace(value.MemberType)), strings.TrimSpace(value.MemberValue), identify}, "\x00")
	}
	data, _ := json.Marshal(item)
	return string(data)
}

func (c *VirtualClusterClient) listFilteredUsersForProfile(ctx context.Context, profile clientProfile, identifier string) ([]IAMUser, error) {
	escaped := escapeIAMFilterValue(identifier)
	filter := fmt.Sprintf(`(status="valid") AND (username="*%s*" OR id="*%s*" OR name="*%s*")`, escaped, escaped, escaped)
	return collectIAMPages(ctx, defaultPageLimit, 0, false, func(token string, size int) (iamPage[IAMUser], error) {
		u, _ := url.Parse(profile.IAMBaseURL)
		u.Path = "/iam/idp/v1/users"
		q := u.Query()
		q.Set("page_size", strconv.Itoa(size))
		q.Set("page_token", token)
		q.Set("filter", filter)
		u.RawQuery = q.Encode()
		var payload iamUserListResponse
		if err := c.getJSONWithProfile(ctx, profile, u.String(), &payload); err != nil {
			if unsupportedIAMUserFilter(err, u.String()) {
				return iamPage[IAMUser]{}, fmt.Errorf("%w: %w", errUnsupportedIAMUserFilter, err)
			}
			return iamPage[IAMUser]{}, err
		}
		return iamPage[IAMUser]{payload.Users, payload.NextPageToken, payload.TotalSize}, nil
	})
}

func unsupportedIAMUserFilter(err error, requestURL string) bool {
	for _, status := range []int{http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusUnprocessableEntity, http.StatusNotImplemented} {
		if strings.HasPrefix(err.Error(), fmt.Sprintf("request %s returned %d:", requestURL, status)) {
			return true
		}
	}
	return false
}
