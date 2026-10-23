package service

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func specVM(kind, name, uid, tenant string, template bool) *unstructured.Unstructured {
	domain := map[string]any{"cpu": map[string]any{"sockets": int64(16), "cores": int64(1), "threads": int64(1)}, "memory": map[string]any{"guest": "32Gi"}}
	spec := map[string]any{"domain": domain}
	if template {
		spec = map[string]any{"template": map[string]any{"spec": spec}}
	}
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "kubevirt.io/v1", "kind": kind, "metadata": map[string]any{"name": name, "namespace": "ns-" + tenant, "labels": map[string]any{"resourcemanager_id": uid}, "annotations": map[string]any{"resource.compute.sensecore.cn/tenant-id": tenant}}, "spec": spec}}
}

func TestMissingECSSpecUsesTargetedVMIAndStoppedVMWithoutOverwritingSKU(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{kubevirtVMGVR: "VirtualMachineList", kubevirtVMIGVR: "VirtualMachineInstanceList"},
		specVM("VirtualMachineInstance", "running", "running-uid", "a", false),
		specVM("VirtualMachine", "stopped", "stopped-uid", "b", true),
		specVM("VirtualMachineInstance", "wrong-tenant", "guarded-uid", "wrong", false),
	)
	items := []ECSListItem{{Name: "running", UID: "running-uid", TenantID: "a"}, {Name: "stopped", UID: "stopped-uid", TenantID: "b"}, {Name: "sku", UID: "sku-uid", MachineType: "c1k.4xlarge"}, {Name: "guarded", UID: "guarded-uid", TenantID: "expected"}}
	warnings := enrichECSMachineSpecsWithClient(context.Background(), items, client)
	if items[0].MachineType != "16 vCPU / 32Gi" || items[1].MachineType != "16 vCPU / 32Gi" || items[2].MachineType != "c1k.4xlarge" || items[3].MachineType != "" || len(warnings) != 1 {
		t.Fatalf("items=%#v warnings=%v", items, warnings)
	}
	for _, action := range client.Actions() {
		list, ok := action.(ktesting.ListAction)
		if !ok || !strings.Contains(list.GetListRestrictions().Labels.String(), "resourcemanager_id=") || strings.Contains(list.GetListRestrictions().Labels.String(), "sku-uid") {
			t.Fatalf("query was not restricted to a missing resource UID: %v", action)
		}
	}
	contexts := buildVMIResourceContexts([]unstructured.Unstructured{*specVM("VirtualMachineInstance", "running", "running-uid", "a", false)})
	if len(contexts) != 1 || contexts[0].MachineSpec != "16 vCPU / 32Gi" {
		t.Fatalf("get context spec missing: %#v", contexts)
	}
}

func TestVMMachineSpecDoesNotGuessIncompleteResources(t *testing.T) {
	vm := specVM("VirtualMachineInstance", "demo", "uid", "tenant", false)
	unstructured.RemoveNestedField(vm.Object, "spec", "domain", "memory")
	if value := vmMachineSpec(vm.Object, false); value != "" {
		t.Fatalf("incomplete spec was guessed: %s", value)
	}
	unstructured.SetNestedField(vm.Object, "16", "spec", "domain", "resources", "requests", "cpu")
	unstructured.SetNestedField(vm.Object, "34359738368", "spec", "domain", "resources", "requests", "memory")
	unstructured.RemoveNestedField(vm.Object, "spec", "domain", "cpu")
	if value := vmMachineSpec(vm.Object, false); !strings.HasPrefix(value, "16 vCPU / ") {
		t.Fatalf("resource fallback=%q", value)
	}
}
