package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestIAMBindingPaginationRecoversOverlappingPagesWithLargerRescan(t *testing.T) {
	for _, total := range []int{250, 1250} {
		t.Run(strconv.Itoa(total), func(t *testing.T) {
			calls, retryCalls := 0, 0
			client := &VirtualClusterClient{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				size, _ := strconv.Atoi(req.URL.Query().Get("pageSize"))
				page, _ := strconv.Atoi(req.URL.Query().Get("page_token"))
				if req.URL.Query().Get("filter") != `(scope="*/target*")` || req.URL.Query().Get("order_by") != "create_time desc" {
					t.Fatalf("rescan changed policy lookup: %s", req.URL)
				}
				switch size {
				case 100:
					if retryCalls > 0 || calls > (total+99)/100 {
						return nil, fmt.Errorf("retry must restart at page 1 with a larger page size: %s", req.URL)
					}
				case 1000:
					retryCalls++
					if page != retryCalls {
						return nil, fmt.Errorf("retry page = %d, want %d", page, retryCalls)
					}
				default:
					return nil, fmt.Errorf("unexpected page size %d", size)
				}
				rows := []IAMBindingPolicy{}
				for i := (page - 1) * size; i < min(page*size, total); i++ {
					id := i
					if size == 100 && page == 2 && i == 100 {
						// Equal creation timestamps move one binding across the
						// boundary, duplicating 99 and omitting 100 on every scan.
						id = 99
					}
					rows = append(rows, IAMBindingPolicy{ID: fmt.Sprintf("policy-%04d", id), CreateTime: "2026-10-22T00:00:00Z"})
				}
				next := ""
				if page*size < total {
					next = strconv.Itoa(page + 1)
				}
				body, err := json.Marshal(iamBindingPolicyListResponse{Policies: rows, NextPageToken: next, TotalSize: total})
				if err != nil {
					return nil, err
				}
				return jsonHTTPResponse(req, string(body)), nil
			})}}
			items, err := client.listIAMBindingPoliciesWithProfile(context.Background(), clientProfile{IAMBaseURL: "https://iam.test"}, `(scope="*/target*")`)
			if err != nil || len(items) != total || retryCalls != (total+999)/1000 {
				t.Fatalf("items=%d calls=%d retryCalls=%d err=%v", len(items), calls, retryCalls, err)
			}
			for i, item := range items {
				if item.ID != fmt.Sprintf("policy-%04d", i) {
					t.Fatalf("binding %d = %q, missing or duplicated binding", i, item.ID)
				}
			}
		})
	}
}

func TestIAMBindingPaginationRejectsIncompleteLargerRescan(t *testing.T) {
	calls := 0
	client := &VirtualClusterClient{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Query().Get("pageSize") == "1000" {
			if req.URL.Query().Get("page_token") != "1" {
				t.Fatal("larger rescan must restart from the first page")
			}
			return jsonHTTPResponse(req, `{"policies":[{"id":"a"},{"id":"b"},{"id":"c"}],"total_size":4}`), nil
		}
		if req.URL.Query().Get("page_token") == "1" {
			return jsonHTTPResponse(req, `{"policies":[{"id":"a"},{"id":"b"}],"total_size":4,"next_page_token":"2"}`), nil
		}
		return jsonHTTPResponse(req, `{"policies":[{"id":"c"}],"total_size":4}`), nil
	})}}
	items, err := client.listIAMBindingPoliciesWithProfile(context.Background(), clientProfile{IAMBaseURL: "https://iam.test"}, "")
	if err == nil || !strings.Contains(err.Error(), "无法校验完整性") || items != nil || calls != 3 {
		t.Fatalf("items=%v calls=%d err=%v", items, calls, err)
	}
}

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
