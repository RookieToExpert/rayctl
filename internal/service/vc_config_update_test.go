package service

import (
	"context"
	"errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"maps"
	"strings"
	"testing"
	"time"

	"rayctl/internal/platform"
)

type fakeVCUpdateAPI struct {
	*fakeVCConfigAPI
	available []platform.VCControlPlaneFlavor
	writes    int
	writeErr  error
}

func (f *fakeVCUpdateAPI) Flavors(context.Context) ([]platform.VCControlPlaneFlavor, error) {
	flavors := make([]platform.VCControlPlaneFlavor, len(f.available))
	for i, v := range f.available {
		flavors[i] = v
		flavors[i].ResourceRequests = maps.Clone(v.ResourceRequests)
		flavors[i].ResourceLimits = maps.Clone(v.ResourceLimits)
	}
	return flavors, nil
}
func (f *fakeVCUpdateAPI) UpdateFlavor(_ context.Context, uid, name string) (*platform.VCOperationResource, error) {
	f.writes++
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	for _, v := range f.available {
		if v.Name == name {
			flavor := v
			f.flavor = &flavor
		}
	}
	return &platform.VCOperationResource{UID: uid, ControlPlaneFlavor: f.flavor}, nil
}
func updateFixture() (*VCConfigService, *fakeVCUpdateAPI) {
	svc, _, _ := configFixture("medium", 3, "8", "32Gi")
	api := &fakeVCUpdateAPI{fakeVCConfigAPI: svc.api.(*fakeVCConfigAPI)}
	api.flavor.MaxNodeCount = 150
	api.available = []platform.VCControlPlaneFlavor{*api.flavor, {Name: "large", Replicas: 3, MaxNodeCount: 500, ResourceRequests: map[string]string{"cpu": "32", "memory": "128Gi"}, ResourceLimits: map[string]string{"cpu": "32", "memory": "128Gi"}}}
	svc.api = api
	return svc, api
}
func prepareConfigUpdate(t *testing.T, svc *VCConfigService, flavor string) *VCConfigUpdateResult {
	t.Helper()
	plan, err := svc.PrepareUpdate(context.Background(), "vc-demo", vcConfigUID, "d", flavor, false)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestVCConfigUpdateDryRunAndUnchangedIssueNoWrites(t *testing.T) {
	svc, api := updateFixture()
	plan := prepareConfigUpdate(t, svc, "large")
	plan.DryRun = true
	if err := svc.ApplyUpdate(context.Background(), plan, true); err != nil || api.writes != 0 {
		t.Fatalf("writes=%d err=%v", api.writes, err)
	}
	plan = prepareConfigUpdate(t, svc, "medium")
	if plan.Changed {
		t.Fatal("same flavor marked as change")
	}
	if err := svc.ApplyUpdate(context.Background(), plan, true); err != nil || api.writes != 0 || plan.Observed == nil || !plan.Observed.Converged {
		t.Fatalf("plan=%+v writes=%d err=%v", plan, api.writes, err)
	}
}

func TestVCConfigUpdateAcceptedIsDistinctFromConverged(t *testing.T) {
	svc, api := updateFixture()
	plan := prepareConfigUpdate(t, svc, "large")
	if err := svc.ApplyUpdate(context.Background(), plan, false); err != nil || api.writes != 1 || plan.Observed != nil || strings.Contains(plan.Status, "已收敛") {
		t.Fatalf("plan=%+v writes=%d err=%v", plan, api.writes, err)
	}
	svc, api = updateFixture()
	plan = prepareConfigUpdate(t, svc, "large")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := svc.ApplyUpdate(ctx, plan, true)
	if !errors.Is(err, context.DeadlineExceeded) || api.writes != 1 || plan.Observed == nil || plan.Observed.AppliedFlavor != "medium" || plan.Observed.Converged {
		t.Fatalf("plan=%+v writes=%d err=%v", plan, api.writes, err)
	}
}

func TestVCConfigUpdatePreflightDetectsConcurrentChanges(t *testing.T) {
	for _, definition := range []bool{false, true} {
		svc, api := updateFixture()
		plan := prepareConfigUpdate(t, svc, "large")
		if definition {
			api.available[1].ResourceLimits["cpu"] = "64"
		} else {
			api.flavor = &platform.VCControlPlaneFlavor{Name: "small"}
		}
		if err := svc.ApplyUpdate(context.Background(), plan, false); err == nil || api.writes != 0 {
			t.Fatalf("definition=%v writes=%d err=%v", definition, api.writes, err)
		}
	}
}

func TestVCConfigUpdateRetainsWriteFailureAndCancellation(t *testing.T) {
	svc, api := updateFixture()
	plan := prepareConfigUpdate(t, svc, "large")
	api.writeErr = errors.New("gateway 504")
	if err := svc.ApplyUpdate(context.Background(), plan, true); err == nil || !strings.Contains(err.Error(), "504") || !strings.Contains(plan.Status, "未确认") || api.writes != 1 {
		t.Fatalf("writes=%d status=%s err=%v", api.writes, plan.Status, err)
	}
	svc, api = updateFixture()
	plan = prepareConfigUpdate(t, svc, "large")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := svc.ApplyUpdate(ctx, plan, false); !errors.Is(err, context.Canceled) || api.writes != 0 {
		t.Fatalf("writes=%d err=%v", api.writes, err)
	}
}

func TestVCConfigUpdateValidatesFlavorBeforeReads(t *testing.T) {
	svc := &VCConfigService{}
	for _, flavor := range []string{"huge", "", "unlimit"} {
		if _, err := svc.PrepareUpdate(context.Background(), "vc-demo", vcConfigUID, "d", flavor, false); err == nil {
			t.Fatalf("accepted %q", flavor)
		}
	}
	if err := ValidateVCFlavor("unlimit", true); err != nil {
		t.Fatal(err)
	}
	svc, _ = updateFixture()
	if _, err := svc.PrepareUpdate(context.Background(), "vc-demo", vcConfigUID, "d", "small", false); err == nil {
		t.Fatal("accepted flavor unavailable on server")
	}
}

func TestVCConfigUpdateWaitRecognizesCompletedTargetRollout(t *testing.T) {
	svc, kube, dynamicClient := configFixture("medium", 3, "8", "32Gi")
	api := &fakeVCUpdateAPI{fakeVCConfigAPI: svc.api.(*fakeVCConfigAPI)}
	api.flavor.MaxNodeCount = 150
	api.available = []platform.VCControlPlaneFlavor{*api.flavor, {Name: "large", Replicas: 3, MaxNodeCount: 500, ResourceRequests: map[string]string{"cpu": "32", "memory": "128Gi"}, ResourceLimits: map[string]string{"cpu": "32", "memory": "128Gi"}}}
	svc.api = api
	plan := prepareConfigUpdate(t, svc, "large")
	ns := "vc-" + vcConfigUID
	cp, _ := dynamicClient.Resource(vcControlPlaneResource).Namespace(ns).Get(context.Background(), ns, metav1.GetOptions{})
	cp.SetLabels(map[string]string{vcFlavorLabel: "large"})
	resources := map[string]interface{}{"cpu": "32", "memory": "128Gi"}
	_ = unstructured.SetNestedMap(cp.Object, resources, "spec", "controlPlane", "statefulSet", "resources", "requests")
	_ = unstructured.SetNestedMap(cp.Object, resources, "spec", "controlPlane", "statefulSet", "resources", "limits")
	_, _ = dynamicClient.Resource(vcControlPlaneResource).Namespace(ns).Update(context.Background(), cp, metav1.UpdateOptions{})
	deployment, _ := kube.AppsV1().Deployments(ns).Get(context.Background(), ns, metav1.GetOptions{})
	deployment.Spec.Template.Spec.Containers[0].Resources.Requests = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("32"), corev1.ResourceMemory: resource.MustParse("128Gi")}
	deployment.Spec.Template.Spec.Containers[0].Resources.Limits = deployment.Spec.Template.Spec.Containers[0].Resources.Requests.DeepCopy()
	_, _ = kube.AppsV1().Deployments(ns).Update(context.Background(), deployment, metav1.UpdateOptions{})
	if err := svc.ApplyUpdate(context.Background(), plan, true); err != nil || api.writes != 1 || plan.Observed == nil || !plan.Observed.Converged {
		t.Fatalf("plan=%+v writes=%d err=%v", plan, api.writes, err)
	}
}
