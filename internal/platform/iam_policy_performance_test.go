package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIAMSubjectPolicyChunksRunConcurrentlyWithBound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entered := make(chan int, 6)
	release := make(chan struct{})
	var active, peak, calls atomic.Int32
	client := iamPolicyTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for prior := peak.Load(); current > prior; prior = peak.Load() {
			if peak.CompareAndSwap(prior, current) {
				break
			}
		}
		chunk, err := iamPolicyRequestChunk(r, 6)
		if err != nil {
			return nil, err
		}
		calls.Add(1)
		entered <- chunk
		select {
		case <-release:
			return iamPolicyTestResponse(r, fmt.Sprintf(`{"policies":[{"id":"policy-%d"}]}`, chunk)), nil
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}))
	done := make(chan iamPolicyTestResult, 1)
	go func() {
		policies, err := client.ListIAMBindingPoliciesForSubjectsProfile(ctx, "cloud", iamPolicyTestIdentifiers(6*iamSubjectPolicyBatchSize))
		done <- iamPolicyTestResult{policies, err}
	}()
	for range 4 {
		awaitIAMPolicyChunk(t, ctx, entered)
	}
	// Hold all four requests at a barrier: a fifth request must stay queued.
	select {
	case chunk := <-entered:
		t.Fatalf("chunk %d started while four requests were still blocked", chunk)
	case <-time.After(50 * time.Millisecond):
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	close(release)
	result := awaitIAMPolicyResult(t, ctx, done)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if peak.Load() != 4 || calls.Load() != 6 {
		t.Fatalf("peak concurrent requests = %d, calls = %d; want 4 and 6", peak.Load(), calls.Load())
	}
	var ids []string
	for _, policy := range result.policies {
		ids = append(ids, policy.ID)
	}
	want := []string{"policy-0", "policy-1", "policy-2", "policy-3", "policy-4", "policy-5"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("policy order = %v, want %v", ids, want)
	}
}

func TestIAMSubjectPolicyChunksPreservePaginationFiltersAndDeduplication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entered := make(chan int, 3)
	completed := make(chan int, 3)
	gates := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	var calls atomic.Int32
	client := iamPolicyTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		chunk, err := iamPolicyRequestChunk(r, 3)
		if err != nil {
			return nil, err
		}
		calls.Add(1)
		filter := r.URL.Query().Get("filter")
		if strings.Count(filter, "member_identify=") != iamSubjectPolicyBatchSize || strings.Count(filter, "scope=") != iamSubjectPolicyBatchSize || strings.Contains(filter, "role_name=") {
			return nil, fmt.Errorf("unexpected subject filter: %q", filter)
		}
		if chunk == 0 && !strings.Contains(filter, `member_identify="*quote\"\\name*" OR scope="*quote\"\\name*"`) {
			return nil, fmt.Errorf("filter lost escaped wildcard lookup: %q", filter)
		}
		page := r.URL.Query().Get("page_token")
		if page == "next-zero" && chunk == 0 {
			return iamPolicyTestResponse(r, `{"policies":[{"id":"policy-page-two"},{"scope":"/common","member_type":"group","member_value":"value","member_identify":"same"}]}`), nil
		}
		if page != "1" {
			return nil, fmt.Errorf("unexpected page token %q for chunk %d", page, chunk)
		}
		entered <- chunk
		select {
		case <-gates[chunk]:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		completed <- chunk
		switch chunk {
		case 0:
			return iamPolicyTestResponse(r, `{"policies":[{"id":"common","member_name":"first"},{"id":"policy-zero"}],"next_page_token":"next-zero"}`), nil
		case 1:
			return iamPolicyTestResponse(r, `{"policies":[{"id":"common","member_name":"second"},{"id":"policy-one"},{"scope":"/common","member_type":"group","member_value":"value","member_identify":"same"}]}`), nil
		default:
			return iamPolicyTestResponse(r, `{"policies":[{"id":"policy-two"}]}`), nil
		}
	}))
	identifiers := iamPolicyTestIdentifiers(3 * iamSubjectPolicyBatchSize)
	identifiers[1] = `quote"\name`
	identifiers = append(identifiers, " ", " "+strings.ToUpper(identifiers[iamSubjectPolicyBatchSize])+" ", ` QUOTE"\NAME `)
	done := make(chan iamPolicyTestResult, 1)
	go func() {
		policies, err := client.ListIAMBindingPoliciesForSubjectsProfile(ctx, "cloud", identifiers)
		done <- iamPolicyTestResult{policies, err}
	}()
	for range 3 {
		awaitIAMPolicyChunk(t, ctx, entered)
	}
	// Return later chunks first, while the first chunk also has a second page.
	for chunk := 2; chunk >= 0; chunk-- {
		close(gates[chunk])
		if got := awaitIAMPolicyChunk(t, ctx, completed); got != chunk {
			t.Fatalf("completed chunk = %d, want %d", got, chunk)
		}
	}
	result := awaitIAMPolicyResult(t, ctx, done)
	if result.err != nil {
		t.Fatal(result.err)
	}
	want := []IAMBindingPolicy{
		{ID: "common", MemberName: "first"},
		{ID: "policy-zero"},
		{ID: "policy-page-two"},
		{Scope: "/common", MemberType: "group", MemberValue: "value", MemberIdentify: "same"},
		{ID: "policy-one"},
		{ID: "policy-two"},
	}
	if !reflect.DeepEqual(result.policies, want) || calls.Load() != 4 {
		t.Fatalf("policies = %#v, calls = %d; want %#v and 4", result.policies, calls.Load(), want)
	}
}

func TestIAMSubjectPolicyChunksDiscardPartialResultsOnError(t *testing.T) {
	lookupErr := errors.New("binding policy lookup failed")
	client := iamPolicyTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		chunk, err := iamPolicyRequestChunk(r, 2)
		if err != nil {
			return nil, err
		}
		if chunk == 1 {
			return nil, lookupErr
		}
		return iamPolicyTestResponse(r, `{"policies":[{"id":"partial-policy"}]}`), nil
	}))
	policies, err := client.ListIAMBindingPoliciesForSubjectsProfile(context.Background(), "cloud", iamPolicyTestIdentifiers(2*iamSubjectPolicyBatchSize))
	if !errors.Is(err, lookupErr) || policies != nil {
		t.Fatalf("policies = %#v, error = %v; want nil and lookup error", policies, err)
	}
}

func TestIAMSubjectPolicyChunksHonorCancellationBeforeQueuedRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entered := make(chan int, 6)
	var calls atomic.Int32
	client := iamPolicyTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		entered <- 0
		<-r.Context().Done()
		return nil, r.Context().Err()
	}))
	done := make(chan iamPolicyTestResult, 1)
	go func() {
		policies, err := client.ListIAMBindingPoliciesForSubjectsProfile(ctx, "cloud", iamPolicyTestIdentifiers(6*iamSubjectPolicyBatchSize))
		done <- iamPolicyTestResult{policies, err}
	}()
	for range 4 {
		awaitIAMPolicyChunk(t, ctx, entered)
	}
	cancel()
	watchCtx, stopWatch := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopWatch()
	result := awaitIAMPolicyResult(t, watchCtx, done)
	if !errors.Is(result.err, context.Canceled) || result.policies != nil || calls.Load() != 4 {
		t.Fatalf("policies = %#v, error = %v, calls = %d; want nil, cancellation and 4", result.policies, result.err, calls.Load())
	}
}

func TestIAMBindingPoliciesStopAtTotalSizeDespiteTrailingCursor(t *testing.T) {
	var calls atomic.Int32
	client := iamPolicyTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		page, err := strconv.Atoi(r.URL.Query().Get("page_token"))
		if err != nil || page < 1 || page > 2 {
			return nil, fmt.Errorf("unnecessary trailing page request: %s", r.URL)
		}
		// The server increments next_page_token even on the final page.
		const total = 176
		start := (page - 1) * defaultPageLimit
		policies := make([]IAMBindingPolicy, 0, defaultPageLimit)
		for i := start; i < min(start+defaultPageLimit, total); i++ {
			policies = append(policies, IAMBindingPolicy{ID: fmt.Sprintf("policy-%03d", i)})
		}
		body, err := json.Marshal(iamBindingPolicyListResponse{
			Policies: policies, NextPageToken: strconv.Itoa(page + 1), TotalSize: total,
		})
		if err != nil {
			return nil, err
		}
		return iamPolicyTestResponse(r, string(body)), nil
	}))
	policies, err := client.ListIAMBindingPoliciesForProfile(context.Background(), "cloud")
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || len(policies) != 176 {
		t.Fatalf("requests = %d, policies = %d; want 2 and 176", calls.Load(), len(policies))
	}
	for i, policy := range policies {
		if want := fmt.Sprintf("policy-%03d", i); policy.ID != want {
			t.Fatalf("policy %d = %q, want %q", i, policy.ID, want)
		}
	}
}

func TestIAMSubjectPoliciesDeduplicateBindingsRatherThanSharedPolicyIDs(t *testing.T) {
	base := IAMBindingPolicy{ID: "shared-policy", Scope: "/rm/shared", MemberType: "GROUP", MemberValue: "group-a", MemberIdentify: "alias-a"}
	tests := []struct {
		name   string
		first  IAMBindingPolicy
		second IAMBindingPolicy
		keep   int
	}{
		{name: "different members", first: base, second: IAMBindingPolicy{ID: base.ID, Scope: base.Scope, MemberType: base.MemberType, MemberValue: "group-b"}, keep: 2},
		{name: "different scopes", first: base, second: IAMBindingPolicy{ID: base.ID, Scope: "/rm/other", MemberType: base.MemberType, MemberValue: base.MemberValue}, keep: 2},
		{name: "different member types", first: base, second: IAMBindingPolicy{ID: base.ID, Scope: base.Scope, MemberType: "USER", MemberValue: base.MemberValue}, keep: 2},
		{name: "same binding different aliases", first: base, second: IAMBindingPolicy{ID: base.ID, Scope: base.Scope, MemberType: "group", MemberValue: base.MemberValue, MemberIdentify: "another-alias"}, keep: 1},
		{name: "member identifier fallback", first: IAMBindingPolicy{ID: base.ID, Scope: base.Scope, MemberType: base.MemberType, MemberIdentify: "group-a"}, second: IAMBindingPolicy{ID: base.ID, Scope: base.Scope, MemberType: base.MemberType, MemberIdentify: "group-b"}, keep: 2},
		{name: "missing policy IDs", first: IAMBindingPolicy{Scope: base.Scope, MemberType: base.MemberType, MemberValue: "group-a"}, second: IAMBindingPolicy{Scope: base.Scope, MemberType: base.MemberType, MemberValue: "group-b"}, keep: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := iamPolicyTestClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
				chunk, err := iamPolicyRequestChunk(r, 2)
				if err != nil {
					return nil, err
				}
				policies := []IAMBindingPolicy{test.first}
				if chunk == 1 {
					policies = append(policies, test.second)
				}
				body, err := json.Marshal(iamBindingPolicyListResponse{Policies: policies})
				if err != nil {
					return nil, err
				}
				return iamPolicyTestResponse(r, string(body)), nil
			}))
			policies, err := client.ListIAMBindingPoliciesForSubjectsProfile(context.Background(), "cloud", iamPolicyTestIdentifiers(2*iamSubjectPolicyBatchSize))
			if err != nil {
				t.Fatal(err)
			}
			want := []IAMBindingPolicy{test.first}
			if test.keep == 2 {
				want = append(want, test.second)
			}
			if !reflect.DeepEqual(policies, want) {
				t.Fatalf("bindings = %#v, want %#v", policies, want)
			}
		})
	}
}

type iamPolicyTestResult struct {
	policies []IAMBindingPolicy
	err      error
}

func iamPolicyTestClient(transport http.RoundTripper) *VirtualClusterClient {
	return &VirtualClusterClient{
		currentProfile: "local",
		profiles: map[string]clientProfile{
			"local": {Name: "local", AccessKey: "local-ak", SecretKey: "local-sk", IAMBaseURL: "https://local.example.test"},
			"cloud": {Name: "cloud", AccessKey: "cloud-ak", SecretKey: "cloud-sk", IAMBaseURL: "https://cloud.example.test"},
		},
		httpClient: &http.Client{Transport: transport},
	}
}

func iamPolicyTestIdentifiers(count int) []string {
	identifiers := make([]string, count)
	for i := range identifiers {
		identifiers[i] = fmt.Sprintf("subject-%03d", i)
	}
	return identifiers
}

func iamPolicyRequestChunk(r *http.Request, chunks int) (int, error) {
	if r.URL.Host != "cloud.example.test" || r.URL.Path != "/iam/authz/v1/bindingPolicies" || !strings.Contains(r.Header.Get("Authorization"), `accesskey="cloud-ak"`) {
		return 0, fmt.Errorf("unexpected profile or endpoint: %s", r.URL)
	}
	if r.URL.Query().Get("order_by") != "create_time desc" || r.URL.Query().Get("pageSize") != fmt.Sprint(defaultPageLimit) {
		return 0, fmt.Errorf("unexpected policy pagination: %s", r.URL.RawQuery)
	}
	filter := r.URL.Query().Get("filter")
	for chunk := range chunks {
		if strings.Contains(filter, fmt.Sprintf(`member_identify="*subject-%03d*"`, chunk*iamSubjectPolicyBatchSize)) {
			return chunk, nil
		}
	}
	return 0, fmt.Errorf("unexpected subject chunk filter: %q", filter)
}

func iamPolicyTestResponse(r *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func awaitIAMPolicyChunk(t *testing.T, ctx context.Context, entered <-chan int) int {
	t.Helper()
	select {
	case chunk := <-entered:
		return chunk
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return 0
	}
}

func awaitIAMPolicyResult(t *testing.T, ctx context.Context, done <-chan iamPolicyTestResult) iamPolicyTestResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return iamPolicyTestResult{}
	}
}
