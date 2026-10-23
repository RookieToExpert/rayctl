package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/dynamic"

	"rayctl/internal/kube"
	"rayctl/internal/platform"
)

func ecsMachineType(vm platform.ECSVirtualMachine) string {
	return strings.TrimSpace(vm.Properties.MachineType)
}
func aisMachineType(ais platform.AISpace) string {
	return strings.TrimSpace(ais.Properties.VirtualMachineProperties.MachineType)
}

// VM topology and guest memory describe the machine itself, without the
// hypervisor's Pod overhead. Never infer a product SKU from CPU/memory alone.
func vmMachineSpec(object map[string]any, template bool) string {
	path := []string{"spec", "domain"}
	if template {
		path = []string{"spec", "template", "spec", "domain"}
	}
	domain, _, _ := unstructured.NestedMap(object, path...)
	var cpu int64 = 1
	haveTopology := false
	for _, field := range []string{"sockets", "cores", "threads"} {
		value, _, _ := unstructured.NestedInt64(domain, "cpu", field)
		if value > 0 {
			cpu *= value
			haveTopology = true
		}
	}
	cpuText := ""
	if haveTopology {
		cpuText = strconv.FormatInt(cpu, 10)
	} else {
		for _, kind := range []string{"requests", "limits"} {
			value, _, _ := unstructured.NestedString(domain, "resources", kind, "cpu")
			if q, err := resource.ParseQuantity(value); err == nil && q.Sign() > 0 {
				cpuText = strconv.FormatFloat(q.AsApproximateFloat64(), 'f', -1, 64)
				break
			}
		}
	}
	memory, _, _ := unstructured.NestedString(domain, "memory", "guest")
	for _, kind := range []string{"requests", "limits"} {
		if memory != "" {
			break
		}
		memory, _, _ = unstructured.NestedString(domain, "resources", kind, "memory")
	}
	q, err := resource.ParseQuantity(memory)
	if cpuText == "" || err != nil || q.Sign() <= 0 {
		return ""
	}
	return cpuText + " vCPU / " + q.String()
}

func enrichECSMachineSpecs(ctx context.Context, items []ECSListItem, kubeconfig string) []string {
	missing := false
	for _, item := range items {
		missing = missing || item.MachineType == ""
	}
	if !missing {
		return nil
	}
	client, err := kube.NewDynamicClient(kubeconfig)
	if err != nil {
		return []string{fmt.Sprintf("resolve missing VM specs: %v", err)}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return enrichECSMachineSpecsWithClient(ctx, items, client)
}

func enrichECSMachineSpecsWithClient(ctx context.Context, items []ECSListItem, client dynamic.Interface) []string {
	type specResult struct {
		index int
		spec  string
		err   error
	}
	indexes := make([]int, 0)
	for i := range items {
		if items[i].MachineType == "" {
			indexes = append(indexes, i)
		}
	}
	lookups := boundedMap(ctx, indexes, 4, func(ctx context.Context, index int) specResult {
		item := items[index]
		result := specResult{index: index}
		if item.UID == "" {
			result.err = fmt.Errorf("resource UID unavailable")
			return result
		}
		options := metav1.ListOptions{LabelSelector: labels.Set{"resourcemanager_id": item.UID}.AsSelector().String(), Limit: 100}
		for _, gvr := range []struct {
			template bool
			vmi      bool
		}{{vmi: true}, {template: true}} {
			resource := kubevirtVMGVR
			if gvr.vmi {
				resource = kubevirtVMIGVR
			}
			objects, err := client.Resource(resource).Namespace(metav1.NamespaceAll).List(ctx, options)
			if err != nil {
				result.err = err
				return result
			}
			if objects.GetContinue() != "" {
				result.err = fmt.Errorf("more than 100 VMs matched one resource UID")
				return result
			}
			for _, object := range objects.Items {
				if object.GetLabels()["resourcemanager_id"] != item.UID {
					continue
				}
				tenant := object.GetAnnotations()["resource.compute.sensecore.cn/tenant-id"]
				if tenant != "" && item.TenantID != "" && tenant != item.TenantID {
					continue
				}
				if uid := object.GetAnnotations()["resource.compute.sensecore.cn/uid"]; uid != "" && uid != item.UID {
					continue
				}
				spec := vmMachineSpec(object.Object, gvr.template)
				if spec == "" {
					continue
				}
				if result.spec != "" && result.spec != spec {
					result.spec = ""
					result.err = fmt.Errorf("conflicting VM specifications for resource UID")
					return result
				}
				result.spec = spec
			}
			if result.spec != "" {
				return result
			}
		}
		result.err = fmt.Errorf("no matching VM CPU/memory configuration found")
		return result
	})
	warnings := make([]string, 0)
	for _, lookup := range lookups {
		if lookup.err != nil {
			warnings = append(warnings, fmt.Sprintf("resolve VM spec for %s/%s: %v", items[lookup.index].ProfileName, items[lookup.index].Name, lookup.err))
		} else {
			items[lookup.index].MachineType = lookup.spec
		}
	}
	return warnings
}
