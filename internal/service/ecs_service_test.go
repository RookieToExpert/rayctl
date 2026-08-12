package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"rayctl/internal/platform"
)

type fakeECSPlatform struct {
	exactAIS          []platform.AISpace
	exactAISByKeyword map[string][]platform.AISpace
	usernames         map[string]string
	fullListCalls     int
	delay             time.Duration
	activeCalls       int32
	maximumCalls      int32
}

func (f *fakeECSPlatform) FindCurrentProfileECSVirtualMachines(context.Context, string) ([]platform.ECSVirtualMachine, error) {
	return nil, nil
}

func (f *fakeECSPlatform) FindCurrentProfileAISpaces(_ context.Context, keyword string) ([]platform.AISpace, error) {
	active := atomic.AddInt32(&f.activeCalls, 1)
	for {
		maximum := atomic.LoadInt32(&f.maximumCalls)
		if active <= maximum || atomic.CompareAndSwapInt32(&f.maximumCalls, maximum, active) {
			break
		}
	}
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	atomic.AddInt32(&f.activeCalls, -1)
	if f.exactAISByKeyword != nil {
		return f.exactAISByKeyword[keyword], nil
	}
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

func TestECSCheckManySharesVMISnapshotAndKeepsInputOrder(t *testing.T) {
	newVMI := func(name string, aisName string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "kubevirt.io/v1",
			"kind":       "VirtualMachineInstance",
			"metadata": map[string]any{
				"name":      name,
				"namespace": "tenant-namespace",
				"annotations": map[string]any{
					"resource.compute.sensecore.cn/name": "ais-" + aisName,
				},
			},
			"status": map[string]any{"nodeName": "host-10-0-0-1"},
		}}
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{kubevirtVMIGVR: "VirtualMachineInstanceList"},
		newVMI("vm-a", "alpha"),
		newVMI("vm-b", "beta"),
		newVMI("vm-c", "gamma"),
	)
	platformClient := &fakeECSPlatform{
		exactAISByKeyword: map[string][]platform.AISpace{
			"alpha": {{Name: "alpha", UID: "uid-a"}},
			"beta":  {{Name: "beta", UID: "uid-b"}},
			"gamma": {{Name: "gamma", UID: "uid-c"}},
		},
		delay: 20 * time.Millisecond,
	}
	ecsService := NewECSServiceWithPlatform(dynamicClient, platformClient)
	identifiers := []string{"gamma", "alpha", "beta"}

	results := ecsService.CheckMany(context.Background(), identifiers, 3)
	for index, result := range results {
		if result.Err != nil {
			t.Fatalf("result[%d] error = %v", index, result.Err)
		}
		if result.Identifier != identifiers[index] || len(result.Result.Items) != 1 || result.Result.Items[0].Name != identifiers[index] {
			t.Fatalf("result[%d]: identifier=%q items=%#v, want %q", index, result.Identifier, result.Result.Items, identifiers[index])
		}
	}
	if maximum := atomic.LoadInt32(&platformClient.maximumCalls); maximum < 2 || maximum > 3 {
		t.Fatalf("maximum platform concurrency = %d, want 2..3", maximum)
	}

	vmiListCalls := 0
	for _, action := range dynamicClient.Actions() {
		if action.GetVerb() == "list" && action.GetResource().Resource == kubevirtVMIGVR.Resource {
			vmiListCalls++
		}
	}
	if vmiListCalls != 1 {
		t.Fatalf("VMI list calls = %d, want 1", vmiListCalls)
	}
}
