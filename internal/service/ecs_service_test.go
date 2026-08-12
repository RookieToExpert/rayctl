package service

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"rayctl/internal/platform"
)

type fakeECSPlatform struct {
	exactAIS      []platform.AISpace
	usernames     map[string]string
	fullListCalls int
}

func (f *fakeECSPlatform) FindCurrentProfileECSVirtualMachines(context.Context, string) ([]platform.ECSVirtualMachine, error) {
	return nil, nil
}

func (f *fakeECSPlatform) FindCurrentProfileAISpaces(context.Context, string) ([]platform.AISpace, error) {
	return f.exactAIS, nil
}

func (f *fakeECSPlatform) ListECSVirtualMachines(context.Context) ([]platform.ECSVirtualMachine, error) {
	f.fullListCalls++
	return nil, nil
}

func (f *fakeECSPlatform) ListAISpaces(context.Context) ([]platform.AISpace, error) {
	f.fullListCalls++
	return nil, nil
}

func (f *fakeECSPlatform) ResolveUsernames(context.Context, []string) (map[string]string, error) {
	return f.usernames, nil
}

func TestBuildVMIResourceContextsSupportsAISMatching(t *testing.T) {
	vmi := unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"name":      "ecs-vm-name",
			"namespace": "tenant-namespace",
			"annotations": map[string]any{
				"resource.compute.sensecore.cn/name": "ais-ais-test",
			},
			"labels": map[string]any{
				"kubevirt.io/nodeName": "host-10-0-0-1",
			},
		},
		"status": map[string]any{
			"nodeName": "host-10-0-0-1",
			"interfaces": []any{map[string]any{
				"ipAddress": "10.119.0.1",
			}},
		},
	}}

	contexts := buildVMIResourceContexts([]unstructured.Unstructured{vmi})
	if len(contexts) != 1 {
		t.Fatalf("contexts = %#v", contexts)
	}
	if contexts[0].VMName != "ecs-vm-name" || contexts[0].InternalIP != "10.119.0.1" {
		t.Fatalf("context = %#v", contexts[0])
	}
	matches := matchVMContextsForAIS(contexts, platform.AISpace{Name: "ais-test"})
	if len(matches) != 1 {
		t.Fatalf("matches = %#v", matches)
	}
}

func TestECSCheckExactAISUsesNarrowPlatformLookup(t *testing.T) {
	vmi := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kubevirt.io/v1",
		"kind":       "VirtualMachineInstance",
		"metadata": map[string]any{
			"name":      "ecs-vm-name",
			"namespace": "tenant-namespace",
			"annotations": map[string]any{
				"resource.compute.sensecore.cn/name": "ais-ais-test",
			},
		},
		"status": map[string]any{"nodeName": "host-10-0-0-1"},
	}}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{kubevirtVMIGVR: "VirtualMachineInstanceList"},
		vmi,
	)
	platformClient := &fakeECSPlatform{
		exactAIS: []platform.AISpace{{
			Name:      "ais-test",
			UID:       "ais-uid",
			CreatorID: "user-id",
		}},
		usernames: map[string]string{"user-id": "test-user"},
	}
	service := NewECSServiceWithPlatform(dynamicClient, platformClient)

	result, err := service.Check(context.Background(), "ais-test")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].VMName != "ecs-vm-name" || result.Items[0].Creator != "test-user" {
		t.Fatalf("result = %#v", result)
	}
	if platformClient.fullListCalls != 0 {
		t.Fatalf("full platform list calls = %d, want 0", platformClient.fullListCalls)
	}
}
