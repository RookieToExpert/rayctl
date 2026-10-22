package platform

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestIAMNumberedPaginationKeepsPageSizeConstant(t *testing.T) {
	client := &VirtualClusterClient{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		size, _ := strconv.Atoi(req.URL.Query().Get("page_size"))
		page, _ := strconv.Atoi(req.URL.Query().Get("page_token"))
		if size != 100 {
			t.Errorf("page %d changed page_size to %d", page, size)
		}
		items := []string{}
		for i := (page - 1) * size; i < min(page*size, 250); i++ {
			items = append(items, fmt.Sprintf(`{"id":"u%03d"}`, i))
		}
		return jsonHTTPResponse(req, fmt.Sprintf(`{"users":[%s],"total_size":250,"next_page_token":"%d"}`, strings.Join(items, ","), page+1)), nil
	})}}
	users, err := client.listUsersForProfileLimit(context.Background(), clientProfile{IAMBaseURL: "https://iam.test"}, 150, false)
	if err != nil || len(users) != 150 || users[149].ID != "u149" {
		t.Fatalf("len=%d err=%v", len(users), err)
	}
}

func TestIAMPaginationRecoversConcurrentInsertByFreshScan(t *testing.T) {
	pass, calls := 0, 0
	items, err := collectIAMPages(context.Background(), 2, 0, false, func(token string, size int) (iamPage[IAMUser], error) {
		calls++
		if token == "1" {
			pass++
			return iamPage[IAMUser]{[]IAMUser{{ID: "a"}, {ID: "b"}}, "2", 3}, nil
		}
		if pass == 1 {
			return iamPage[IAMUser]{[]IAMUser{{ID: "b"}, {ID: "c"}}, "", 4}, nil
		}
		return iamPage[IAMUser]{[]IAMUser{{ID: "c"}}, "", 3}, nil
	})
	if err != nil || pass != 2 || calls != 4 || len(items) != 3 {
		t.Fatalf("items=%v pass=%d calls=%d err=%v", items, pass, calls, err)
	}
}

func TestIAMPaginationDoesNotSilentlyReturnMissingRows(t *testing.T) {
	calls := 0
	items, err := collectIAMPages(context.Background(), 2, 0, false, func(token string, _ int) (iamPage[IAMUser], error) {
		calls++
		if token == "1" {
			return iamPage[IAMUser]{[]IAMUser{{ID: "a"}, {ID: "b"}}, "2", 4}, nil
		}
		return iamPage[IAMUser]{[]IAMUser{{ID: "c"}}, "", 4}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "无法校验完整性") || items != nil || calls != 4 {
		t.Fatalf("items=%v calls=%d err=%v", items, calls, err)
	}
}

func TestIAMPaginationDeduplicatesWithoutDroppingOtherBindings(t *testing.T) {
	base := IAMBindingPolicy{ID: "policy", Scope: "/scope", MemberType: "GROUP", MemberValue: "a"}
	other := base
	other.MemberValue = "b"
	items, err := collectIAMPages(context.Background(), 2, 0, false, func(token string, _ int) (iamPage[IAMBindingPolicy], error) {
		if token == "1" {
			return iamPage[IAMBindingPolicy]{[]IAMBindingPolicy{base}, "2", 2}, nil
		}
		return iamPage[IAMBindingPolicy]{[]IAMBindingPolicy{base, other}, "", 2}, nil
	})
	if err != nil || len(items) != 2 || items[1].MemberValue != "b" {
		t.Fatalf("items=%v err=%v", items, err)
	}
}

func TestIAMPaginationNoProgressIsBounded(t *testing.T) {
	calls := 0
	_, err := collectIAMPages(context.Background(), 1, 0, true, func(token string, _ int) (iamPage[IAMUser], error) {
		calls++
		n, _ := strconv.Atoi(token)
		return iamPage[IAMUser]{[]IAMUser{{ID: "same"}}, strconv.Itoa(n + 1), 0}, nil
	})
	if err == nil || calls != 4 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
