package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

const queueAuthTestUID = "11111111-1111-4111-8111-111111111111"

func queueAuthTestResource(name, uid, subscription, region string) StorageVolumeResource {
	return StorageVolumeResource{
		ID: uid, Name: name, Type: "compute.ssp.v1.queue", Region: region,
		RID: fmt.Sprintf("/subscriptions/%s/resourceGroups/default/regions/%s/clusters/cluster-test/queues/%s", subscription, region, name),
	}
}

func queueAuthTestResponse(r *http.Request, payload any) *http.Response {
	data, _ := json.Marshal(payload)
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(data))), Request: r}
}

func TestQueueAuthUIDLookupPaginatesQueueMetadata(t *testing.T) {
	requests := 0
	target := queueAuthTestResource("custom-queue-name", queueAuthTestUID, "sub", "cn-pj-01")
	client := &VirtualClusterClient{
		currentProfile: "current",
		profiles:       map[string]clientProfile{"current": {Name: "current", AccessKey: "current", SecretKey: "sk", BaseURL: "https://platform.test", Region: "cn-pj-01"}},
		httpClient: &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests++
			if r.Method != http.MethodPost || r.URL.Path != "/rmh/v1/resources:page" {
				t.Errorf("unexpected request %s %s", r.Method, r.URL)
			}
			if r.URL.Query().Get("filter") != `resource_type="compute.ssp.v1.queue"` {
				t.Errorf("UID lookup used an unsupported server filter: %s", r.URL)
			}
			if r.URL.Query().Get("page_size") != "100" {
				t.Errorf("unbounded queue metadata request: %s", r.URL)
			}
			if r.URL.Query().Get("page_token") == "1" {
				return queueAuthTestResponse(r, storageVolumePageResponse{Resources: []StorageVolumeResource{queueAuthTestResource("unrelated", "other", "sub", "cn-pj-01")}, NextPageToken: "2", TotalSize: 2}), nil
			}
			return queueAuthTestResponse(r, storageVolumePageResponse{Resources: []StorageVolumeResource{target}, TotalSize: 2}), nil
		})},
	}
	resource, err := client.FindSSPQueueAuthResource(context.Background(), "ssp-"+queueAuthTestUID)
	if err != nil || resource.RID != target.RID || resource.ProfileName != "current" || requests != 2 {
		t.Fatalf("resource=%+v requests=%d error=%v", resource, requests, err)
	}
}

func TestQueueAuthSelectsVerifiedScopeWithinEnvironment(t *testing.T) {
	current := queueAuthTestResource("queue-target", "current-uid", "sub-current", "cn-pj-01")
	other := queueAuthTestResource("queue-target", "other-uid", "sub-other", "cn-pj-01")
	missingScope := current
	missingScope.RID = ""
	for _, tt := range []struct {
		name        string
		resources   map[string][]StorageVolumeResource
		brokenPage  bool
		denied      bool
		wantProfile string
		wantError   string
		otherReads  bool
	}{
		{name: "current exact match wins", resources: map[string][]StorageVolumeResource{"current": {current}, "other": {other}}, wantProfile: "current"},
		{name: "exact match outranks another tenant fuzzy match", resources: map[string][]StorageVolumeResource{"current": {queueAuthTestResource("queue-target-extra", "extra", "sub-current", "cn-pj-01")}, "other": {other}}, wantProfile: "other", otherReads: true},
		{name: "unique name fragment", resources: map[string][]StorageVolumeResource{"other": {queueAuthTestResource("queue-target-extra", "extra", "sub-other", "cn-pj-01")}}, wantProfile: "other", otherReads: true},
		{name: "ambiguous name fragment", resources: map[string][]StorageVolumeResource{"current": {queueAuthTestResource("queue-target-one", "one", "sub-current", "cn-pj-01")}, "other": {queueAuthTestResource("queue-target-two", "two", "sub-other", "cn-pj-01")}}, wantError: "multiple queues", otherReads: true},
		{name: "deduplicates repeated resource", resources: map[string][]StorageVolumeResource{"current": {current, current}}, wantProfile: "current"},
		{name: "missing RID does not fabricate scope", resources: map[string][]StorageVolumeResource{"current": {missingScope}}, wantError: "no verified resource scope"},
		{name: "wrong region is excluded", resources: map[string][]StorageVolumeResource{"current": {queueAuthTestResource("queue-target", "pt-uid", "sub-current", "cn-pj-03")}}, wantError: "not found", otherReads: true},
		{name: "empty response", resources: map[string][]StorageVolumeResource{}, wantError: "not found", otherReads: true},
		{name: "incomplete pagination fails", resources: map[string][]StorageVolumeResource{"current": {current}}, brokenPage: true, wantError: "invalid pagination token", otherReads: true},
		{name: "profile failure is not an empty authorization result", resources: map[string][]StorageVolumeResource{}, denied: true, wantError: "denied", otherReads: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			reads := map[string]int{}
			client := &VirtualClusterClient{
				currentProfile: "current",
				profiles: map[string]clientProfile{
					"current": {Name: "current", AccessKey: "current", SecretKey: "sk", BaseURL: "https://platform.test", Region: "cn-pj-01"},
					"other":   {Name: "other", AccessKey: "other", SecretKey: "sk", BaseURL: "https://platform.test", Region: "cn-pj-01"},
					"pt":      {Name: "pt", AccessKey: "pt", SecretKey: "sk", BaseURL: "https://platform.test", Region: "cn-pj-03"},
				},
				httpClient: &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
					profile := ""
					for _, name := range []string{"current", "other", "pt"} {
						if strings.Contains(r.Header.Get("Authorization"), `accesskey="`+name+`"`) {
							profile = name
						}
					}
					mu.Lock()
					reads[profile]++
					mu.Unlock()
					if tt.denied && profile == "current" {
						return &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("denied")), Request: r}, nil
					}
					payload := storageVolumePageResponse{Resources: tt.resources[profile]}
					if tt.brokenPage && profile == "current" {
						payload.NextPageToken = "1"
					}
					return queueAuthTestResponse(r, payload), nil
				})},
			}
			resource, err := client.FindSSPQueueAuthResource(context.Background(), "queue-target")
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) || resource != nil {
					t.Fatalf("resource=%+v error=%v; want %s", resource, err, tt.wantError)
				}
			} else if err != nil || resource.ProfileName != tt.wantProfile {
				t.Fatalf("resource=%+v error=%v; want profile %s", resource, err, tt.wantProfile)
			}
			if reads["pt"] != 0 || (reads["other"] > 0) != tt.otherReads {
				t.Fatalf("unexpected profile reads: %v", reads)
			}
		})
	}
}

func TestQueueAuthUIDDoesNotMatchAnotherQueueName(t *testing.T) {
	client := &VirtualClusterClient{
		currentProfile: "current",
		profiles:       map[string]clientProfile{"current": {Name: "current", AccessKey: "current", SecretKey: "sk", BaseURL: "https://platform.test", Region: "cn-pj-01"}},
		httpClient: &http.Client{Transport: sspRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			return queueAuthTestResponse(r, storageVolumePageResponse{Resources: []StorageVolumeResource{
				queueAuthTestResource("queue-"+queueAuthTestUID, "different-uid", "sub", "cn-pj-01"),
			}}), nil
		})},
	}
	resource, err := client.FindSSPQueueAuthResource(context.Background(), queueAuthTestUID)
	if resource != nil || err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("UID matched an unrelated queue name: resource=%+v error=%v", resource, err)
	}
}
