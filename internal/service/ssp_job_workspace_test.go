package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestAITWorkspaceScopeMatchesAIDAndAIR(t *testing.T) {
	client := newSSPQueryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"resources":[{"name":"platform-only","type":"compute.ssp.v1.workspace","rid":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/workspaces/platform-only"},{"name":"shared","type":"compute.ssp.v1.workspace","rid":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-01/workspaces/shared"},{"name":"shared","type":"compute.ssp.v1.workspace","rid":"/subscriptions/other/resourceGroups/default/regions/cn-pj-01/workspaces/shared"}],"total_size":3}`)
	})
	hc := fake.NewClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "hc-only", Labels: map[string]string{sspWorkspaceNameLabel: "skip-webhook-test"}}})
	ait := NewSSPJobService(hc, client)
	// Even a matching HC Pod is placement evidence, not an inventory filter.
	pods := []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{sspWorkspaceNameLabel: "shared"}}}}
	refs, err := ait.resolveWorkspaceCandidatesForRegion(context.Background(), "", pods, "cn-pj-01")
	if err != nil {
		t.Fatal(err)
	}
	aid, err := NewSSPAIDService(hc, client).resolveAIDWorkspaceCandidates(context.Background(), "", "cn-pj-01")
	if err != nil {
		t.Fatal(err)
	}
	keys := func(refs []sspWorkspaceRef) []string {
		result := []string{}
		for _, ref := range refs {
			result = append(result, ref.Subscription+"/"+ref.ProfileName+"/"+ref.Name)
		}
		sort.Strings(result)
		return result
	}
	if len(refs) != 3 || !reflect.DeepEqual(keys(refs), keys(aid)) {
		t.Fatalf("AIT=%v AID=%v", keys(refs), keys(aid))
	}
	for _, ref := range refs {
		if ref.Name == "skip-webhook-test" {
			t.Fatal("HC-only namespace became a platform scope")
		}
	}
	explicit, err := ait.resolveWorkspaceCandidatesForRegion(context.Background(), "direct-unlisted", nil, "cn-pj-01")
	if err != nil || len(explicit) != 1 || explicit[0].Name != "direct-unlisted" {
		t.Fatalf("explicit=%v err=%v", explicit, err)
	}
}

func TestAITPartialWorkspaceFailureRemainsVisible(t *testing.T) {
	for _, found := range []bool{false, true} {
		t.Run(fmt.Sprint(found), func(t *testing.T) {
			client := newSSPQueryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == "/rmh/v1/resources:page":
					writeTestWorkspaces(w, "readable", "denied")
				case strings.Contains(r.URL.Path, "/workspaces/denied/"):
					w.WriteHeader(403)
				case strings.HasSuffix(r.URL.Path, "/trainingJobs"):
					if found {
						fmt.Fprint(w, `{"training_jobs":[{"name":"demo","uid":"job-uid","status":{"state":"SUCCEEDED"}}],"total_size":1}`)
					} else {
						fmt.Fprint(w, `{"training_jobs":[],"total_size":0}`)
					}
				case strings.HasSuffix(r.URL.Path, "/workers"):
					fmt.Fprint(w, `{"workers":[],"total_size":0}`)
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
			})
			result, err := NewSSPJobService(fake.NewClientset(), client).GetJobInRegion(context.Background(), "demo", "", "cn-pj-01", false)
			if found {
				if err != nil || result == nil || result.Name != "demo" || !result.QueryIncomplete || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "denied") || strings.Contains(result.Warnings[0], "http://") {
					t.Fatalf("result=%+v err=%v", result, err)
				}
			} else if result != nil || err == nil || !strings.Contains(err.Error(), "denied") || strings.Contains(err.Error(), "http://") || strings.Contains(err.Error(), "not found") {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestAITCancelledLookupReturnsCancellation(t *testing.T) {
	client := newSSPQueryTestClient(t, func(w http.ResponseWriter, r *http.Request) { writeTestWorkspaces(w, "readable") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := NewSSPJobService(fake.NewClientset(), client).GetJobInRegion(ctx, "missing", "", "cn-pj-01", false)
	if result != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("result=%v err=%v", result, err)
	}
}
