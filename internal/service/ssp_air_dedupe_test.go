package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
)

func TestAIRListDeduplicatesSinglePageBeforeStateFilter(t *testing.T) {
	client := newSSPQueryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/rmh/v1/resources:page" {
			writeTestWorkspaces(w, "ws")
			return
		}
		fmt.Fprint(w, `{"airs":[{"name":"infer-duplicate","uid":"uid-one","status":{"state":"PENDING","update_time":"2026-10-23T01:00:00Z"}},{"name":"infer-duplicate","uid":"uid-one","status":{"state":"RUNNING","update_time":"2026-10-23T02:00:00Z"}},{"name":"infer-duplicate","uid":"uid-two","status":{"state":"RUNNING"}}],"total_size":3}`)
	})
	svc := NewSSPAIRService(client)
	result, err := svc.ListJobs(context.Background(), "cn-pj-01", "ws", "running")
	if err != nil || len(result.Items) != 2 || result.DuplicatesRemoved != 1 || result.Items[0].UID == result.Items[1].UID {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	result, err = svc.ListJobs(context.Background(), "cn-pj-01", "ws", "pending")
	if err != nil || len(result.Items) != 0 {
		t.Fatalf("stale duplicate survived filter: %+v err=%v", result, err)
	}
}

func TestAIRListDeduplicatesAcrossPages(t *testing.T) {
	var pages atomic.Int32
	client := newSSPQueryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/rmh/v1/resources:page" {
			writeTestWorkspaces(w, "ws")
			return
		}
		pages.Add(1)
		skip, _ := strconv.Atoi(r.URL.Query().Get("skip"))
		jobs := []map[string]any{}
		if skip == 0 {
			for i := 0; i < 100; i++ {
				jobs = append(jobs, map[string]any{"name": fmt.Sprintf("infer-%03d", i), "uid": fmt.Sprintf("uid-%03d", i)})
			}
		} else {
			jobs = append(jobs, map[string]any{"name": "infer-099", "uid": "uid-099"}, map[string]any{"name": "infer-100", "uid": "uid-100"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"airs": jobs, "total_size": 102})
	})
	result, err := NewSSPAIRService(client).ListJobs(context.Background(), "cn-pj-01", "ws", "")
	if err != nil || len(result.Items) != 101 || result.DuplicatesRemoved != 1 || pages.Load() != 2 {
		t.Fatalf("count=%d pages=%d err=%v", len(result.Items), pages.Load(), err)
	}
}

func TestAIRUIDDedupDoesNotMergeDistinctObjectsWithSameName(t *testing.T) {
	items := []SSPAIRJobItem{
		{Name: "same", UID: "a", Profile: "profile-one"}, {Name: "same", UID: "a", Profile: "alias-profile"},
		{Name: "same", UID: "b"}, {Name: "same", Workspace: "one", Region: "cn-pj-01"}, {Name: "same", Workspace: "two", Region: "cn-pj-01"}, {Name: "same", Workspace: "one", Region: "cn-pj-03"},
	}
	result := deduplicateAIRJobs(items)
	if len(result) != 5 {
		t.Fatalf("distinct objects merged: %+v", result)
	}
}
