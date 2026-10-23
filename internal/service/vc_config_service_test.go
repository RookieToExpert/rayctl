package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"

	"rayctl/internal/platform"
)

const vcConfigUID = "019d4c85-01c5-7963-b7f7-15f849a934f3"

type fakeVCConfigAPI struct {
	flavor *platform.VCControlPlaneFlavor
	err    error
}

func (f *fakeVCConfigAPI) Get(context.Context, string) (*platform.VCOperationResource, error) {
	return &platform.VCOperationResource{UID: vcConfigUID, Name: "vc-demo", ControlPlaneFlavor: f.flavor}, f.err
}
func (f *fakeVCConfigAPI) Flavors(context.Context) ([]platform.VCControlPlaneFlavor, error) {
	if f.flavor == nil {
		return nil, f.err
	}
	return []platform.VCControlPlaneFlavor{*f.flavor}, f.err
}

func configFixture(flavor string, replicas int32, cpu, memory string) (*VCConfigService, *fake.Clientset, *dynamicfake.FakeDynamicClient) {
	ns := "vc-" + vcConfigUID
	cp := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "controlplane.cluster.x-k8s.io/v1alpha2", "kind": "VCluster2ControlPlane",
		"metadata": map[string]interface{}{"name": ns, "namespace": ns, "uid": "control-plane-uid", "labels": map[string]interface{}{vcFlavorLabel: flavor}},
		"spec": map[string]interface{}{"replicas": int64(replicas), "controlPlane": map[string]interface{}{"statefulSet": map[string]interface{}{"resources": map[string]interface{}{
			"requests": map[string]interface{}{"cpu": cpu, "memory": memory}, "limits": map[string]interface{}{"cpu": cpu, "memory": memory},
		}}}},
	}}
	resources := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu), corev1.ResourceMemory: resource.MustParse(memory)}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: ns, Namespace: ns, UID: types.UID("deployment-uid"), Generation: 5, OwnerReferences: []metav1.OwnerReference{{Kind: "VCluster2ControlPlane", Name: ns, UID: cp.GetUID()}}},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "syncer", Resources: corev1.ResourceRequirements{Requests: resources, Limits: resources.DeepCopy()}}}}}},
		Status:     appsv1.DeploymentStatus{ObservedGeneration: 5, Replicas: replicas, UpdatedReplicas: replicas, ReadyReplicas: replicas, AvailableReplicas: replicas},
	}
	kube := fake.NewSimpleClientset(deployment)
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), cp)
	api := &fakeVCConfigAPI{flavor: &platform.VCControlPlaneFlavor{Name: flavor, Replicas: replicas, ResourceRequests: map[string]string{"cpu": cpu, "memory": memory}, ResourceLimits: map[string]string{"cpu": cpu, "memory": memory}}}
	return NewVCConfigService(kube, dynamicClient, api), kube, dynamicClient
}

func TestVCConfigReadsFlavorWithoutInferringFromReplicaCount(t *testing.T) {
	for _, test := range []struct {
		name        string
		replicas    int32
		cpu, memory string
	}{
		{"minimum", 1, "2", "8Gi"}, {"small", 1, "2", "8Gi"}, {"medium", 3, "8", "32Gi"}, {"large", 3, "32", "128Gi"},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, _, _ := configFixture(test.name, test.replicas, test.cpu, test.memory)
			result, err := svc.Get(context.Background(), "vc-demo", vcConfigUID, "pt")
			if err != nil || !result.Converged || result.AppliedFlavor != test.name || result.Environment != "pt" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestVCConfigRetainsHCLabelWhenPersistentReadFails(t *testing.T) {
	svc, _, _ := configFixture("small", 1, "2", "8Gi")
	svc.api = &fakeVCConfigAPI{err: errors.New("forbidden")}
	result, err := svc.Get(context.Background(), "vc-demo", vcConfigUID, "d")
	if err == nil || !strings.Contains(err.Error(), "forbidden") || result.AppliedFlavor != "small" || result.Desired != nil || result.Converged {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestVCConfigUnknownLabelAndUnrelatedDeploymentAreNotGuessed(t *testing.T) {
	svc, kube, dynamicClient := configFixture("medium", 3, "8", "32Gi")
	ns := "vc-" + vcConfigUID
	cp, _ := dynamicClient.Resource(vcControlPlaneResource).Namespace(ns).Get(context.Background(), ns, metav1.GetOptions{})
	cp.SetLabels(nil)
	_, _ = dynamicClient.Resource(vcControlPlaneResource).Namespace(ns).Update(context.Background(), cp, metav1.UpdateOptions{})
	deployment, _ := kube.AppsV1().Deployments(ns).Get(context.Background(), ns, metav1.GetOptions{})
	deployment.OwnerReferences[0].UID = "another-vc"
	_, _ = kube.AppsV1().Deployments(ns).Update(context.Background(), deployment, metav1.UpdateOptions{})
	result, err := svc.Get(context.Background(), "vc-demo", vcConfigUID, "d")
	if err == nil || result.AppliedFlavor != "" || result.Deployment != nil || result.Converged {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestVCConfigConvergenceRequiresResourcesAndCompletedRollout(t *testing.T) {
	svc, _, _ := configFixture("medium", 3, "8", "32Gi")
	result, err := svc.Get(context.Background(), "vc-demo", vcConfigUID, "d")
	if err != nil {
		t.Fatal(err)
	}
	// Equivalent Kubernetes quantity spellings must compare numerically.
	result.Deployment.Requests["cpu"] = "8000m"
	if !result.ConvergedFor(result.Desired) {
		t.Fatal("equivalent CPU rejected")
	}
	result.Deployment.Limits["memory"] = "128Gi"
	if result.ConvergedFor(result.Desired) {
		t.Fatal("wrong resources accepted")
	}
	result.Deployment.Limits["memory"] = "32Gi"
	result.Deployment.ObservedGeneration = 4
	if result.ConvergedFor(result.Desired) {
		t.Fatal("unobserved rollout accepted")
	}
	result.Deployment.ObservedGeneration = 5
	result.Deployment.Total = 4
	if result.ConvergedFor(result.Desired) {
		t.Fatal("old extra replica accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.Get(ctx, "vc-demo", vcConfigUID, "d"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}
