package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"rayctl/internal/platform"
)

func TestAIDAndAIRUseSameWorkspaceInventory(t *testing.T) {
	var mu sync.Mutex
	queried := []string{}
	client := newSSPQueryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/rmh/v1/resources:page" {
			// No HC namespace exists for platform-only; both subscriptions have
			// a workspace named shared, and neither may be lost by name merging.
			fmt.Fprint(w, `{"resources":[{"name":"platform-only","type":"compute.ssp.v1.workspace","rid":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/workspaces/platform-only"},{"name":"shared","type":"compute.ssp.v1.workspace","rid":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/workspaces/shared"},{"name":"shared","type":"compute.ssp.v1.workspace","rid":"/subscriptions/other/resourceGroups/default/regions/cn-pj-01/workspaces/shared"}],"total_size":3}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/aids") {
			mu.Lock()
			queried = append(queried, r.URL.Path)
			mu.Unlock()
			fmt.Fprint(w, `{"aids":[],"total_size":0}`)
			return
		}
		t.Errorf("unexpected request: %s", r.URL.Path)
		http.NotFound(w, r)
	})
	hc := fake.NewClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-namespace", Labels: map[string]string{sspWorkspaceNameLabel: "hc-only-test"}}},
	)
	aid := NewSSPAIDService(hc, client)
	refs, err := aid.resolveAIDWorkspaceCandidates(context.Background(), "", "cn-pj-01")
	if err != nil {
		t.Fatal(err)
	}
	air, err := NewSSPAIRService(client).resolveWorkspacesWithStatus(context.Background(), "cn-pj-01", "")
	if err != nil {
		t.Fatal(err)
	}
	aidKeys, airKeys := []string{}, []string{}
	for _, ref := range refs {
		aidKeys = append(aidKeys, ref.ProfileName+"/"+ref.Subscription+"/"+ref.Name)
	}
	for _, ref := range air {
		airKeys = append(airKeys, ref.ProfileName+"/"+ref.Subscription+"/"+ref.Name)
	}
	sort.Strings(aidKeys)
	sort.Strings(airKeys)
	if len(aidKeys) != 3 || !reflect.DeepEqual(aidKeys, airKeys) {
		t.Fatalf("AID=%v AIR=%v", aidKeys, airKeys)
	}
	result, err := aid.GetAIDInRegion(context.Background(), "verified-missing", "", "cn-pj-01", false)
	var notFound *AIDNotFoundError
	if result != nil || !errors.As(err, &notFound) {
		t.Fatalf("result=%v err=%v", result, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(queried) != 3 || strings.Contains(strings.Join(queried, " "), "hc-only-test") {
		t.Fatalf("queried scopes: %v", queried)
	}
	if !strings.Contains(strings.Join(queried, " "), "/subscriptions/other/") {
		t.Fatalf("same-name workspace subscription lost: %v", queried)
	}
}

func TestAIDWorkspaceInventoryFailureIsNotNotFound(t *testing.T) {
	client := newSSPQueryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	aid := NewSSPAIDService(fake.NewClientset(), client)
	result, err := aid.GetAIDInRegion(context.Background(), "missing", "", "cn-pj-01", false)
	var notFound *AIDNotFoundError
	var status *platform.HTTPStatusError
	if result != nil || errors.As(err, &notFound) || !errors.As(err, &status) || status.StatusCode != 403 {
		t.Fatalf("result=%v err=%v", result, err)
	}
}

func TestAIDExplicitWorkspaceCanStillUseDirectLookup(t *testing.T) {
	client := newSSPQueryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeTestWorkspaces(w, "listed")
	})
	aid := NewSSPAIDService(fake.NewClientset(), client)
	refs, err := aid.resolveAIDWorkspaceCandidates(context.Background(), "explicit-unlisted", "cn-pj-01")
	if err != nil || len(refs) != 1 || refs[0].Name != "explicit-unlisted" {
		t.Fatalf("refs=%v err=%v", refs, err)
	}
}

func TestAIDAndAIRRetainPartialWorkspaceInventory(t *testing.T) {
	client := newSSPQueryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/rmh/v1/resources:page":
			// One readable workspace, but the inventory claims another page
			// without a token. Neither command may call this complete.
			fmt.Fprint(w, `{"resources":[{"name":"readable","type":"compute.ssp.v1.workspace","rid":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/workspaces/readable"}],"total_size":2}`)
		case strings.HasSuffix(r.URL.Path, "/aids"):
			fmt.Fprint(w, `{"aids":[],"total_size":0}`)
		case strings.HasSuffix(r.URL.Path, "/airs"):
			fmt.Fprint(w, `{"airs":[{"name":"infer-readable","uid":"uid-readable"}],"total_size":1}`)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	aid := NewSSPAIDService(fake.NewClientset(), client)
	result, err := aid.GetAIDInRegion(context.Background(), "missing", "", "cn-pj-01", false)
	var notFound *AIDNotFoundError
	if result != nil || err == nil || errors.As(err, &notFound) || !strings.Contains(err.Error(), "可访问范围内未找到") || !strings.Contains(err.Error(), "分页未完成") {
		t.Fatalf("result=%v err=%v", result, err)
	}
	air, err := NewSSPAIRService(client).ListJobs(context.Background(), "cn-pj-01", "", "")
	if err != nil || air == nil || len(air.Items) != 1 || !air.QueryIncomplete || len(air.Warnings) != 1 || !strings.Contains(air.Warnings[0], "分页未完成") {
		t.Fatalf("result=%+v err=%v", air, err)
	}
}
