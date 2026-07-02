package service

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const (
	disallowPrivilegedContainersPolicyName = "disallow-privileged-containers"
	disallowPrivilegedRuleName             = "privileged-containers"
	policySelectorLabelKey                 = "vcluster.loft.sh/vcluster-name"
)

var clusterPolicyGVR = schema.GroupVersionResource{
	Group:    "kyverno.io",
	Version:  "v1",
	Resource: "clusterpolicies",
}

type PolicyService struct {
	dynamicClient  dynamic.Interface
	clusterService *ClusterService
}

type PolicyUpdateResult struct {
	PolicyName     string
	ClusterName    string
	ClusterUID     string
	RuleName       string
	SelectorKey    string
	SelectorValue  string
	AlreadyPresent bool
	Updated        bool
}

func NewPolicyService(dynamicClient dynamic.Interface, clusterService *ClusterService) *PolicyService {
	return &PolicyService{
		dynamicClient:  dynamicClient,
		clusterService: clusterService,
	}
}

func (s *PolicyService) UpdateClusterPolicy(ctx context.Context, policyName string, clusterIdentifier string) (*PolicyUpdateResult, error) {
	policyName = strings.TrimSpace(policyName)
	if policyName != disallowPrivilegedContainersPolicyName {
		return nil, fmt.Errorf("当前 policy update 仅支持 %q", disallowPrivilegedContainersPolicyName)
	}
	if s.dynamicClient == nil {
		return nil, fmt.Errorf("policy update requires kubernetes dynamic client")
	}
	if s.clusterService == nil {
		return nil, fmt.Errorf("policy update requires cluster service")
	}

	clusterResult, err := s.clusterService.Get(ctx, clusterIdentifier)
	if err != nil {
		return nil, err
	}

	selectorValue := strings.TrimSpace(clusterResult.ControlPlaneNamespace)
	if selectorValue == "" {
		return nil, fmt.Errorf("cluster %q 控制面 namespace 为空", clusterIdentifier)
	}

	policy, err := s.dynamicClient.Resource(clusterPolicyGVR).Get(ctx, policyName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get clusterpolicy %q: %w", policyName, err)
	}

	updatedObj, alreadyPresent, err := ensureDisallowPrivilegedPolicyExclusion(policy, selectorValue)
	if err != nil {
		return nil, err
	}

	result := &PolicyUpdateResult{
		PolicyName:     policyName,
		ClusterName:    clusterResult.ClusterName,
		ClusterUID:     clusterResult.ClusterUID,
		RuleName:       disallowPrivilegedRuleName,
		SelectorKey:    policySelectorLabelKey,
		SelectorValue:  selectorValue,
		AlreadyPresent: alreadyPresent,
		Updated:        !alreadyPresent,
	}
	if alreadyPresent {
		return result, nil
	}

	if _, err := s.dynamicClient.Resource(clusterPolicyGVR).Update(ctx, updatedObj, metav1.UpdateOptions{}); err != nil {
		return nil, fmt.Errorf("update clusterpolicy %q: %w", policyName, err)
	}
	return result, nil
}

func ensureDisallowPrivilegedPolicyExclusion(policy *unstructured.Unstructured, selectorValue string) (*unstructured.Unstructured, bool, error) {
	if policy == nil {
		return nil, false, fmt.Errorf("clusterpolicy is required")
	}

	rules, found, err := unstructured.NestedSlice(policy.Object, "spec", "rules")
	if err != nil {
		return nil, false, fmt.Errorf("read clusterpolicy rules: %w", err)
	}
	if !found || len(rules) == 0 {
		return nil, false, fmt.Errorf("clusterpolicy %q has no spec.rules", policy.GetName())
	}

	ruleIndex := -1
	for i, item := range rules {
		ruleMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(fmt.Sprintf("%v", ruleMap["name"])) == disallowPrivilegedRuleName {
			ruleIndex = i
			break
		}
	}
	if ruleIndex < 0 {
		return nil, false, fmt.Errorf("clusterpolicy %q has no rule named %q", policy.GetName(), disallowPrivilegedRuleName)
	}

	ruleMap, ok := rules[ruleIndex].(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("clusterpolicy rule %q has unexpected format", disallowPrivilegedRuleName)
	}

	excludeMap, _, err := unstructured.NestedMap(ruleMap, "exclude")
	if err != nil {
		return nil, false, fmt.Errorf("read clusterpolicy exclude block: %w", err)
	}
	anyItems, found, err := unstructured.NestedSlice(ruleMap, "exclude", "any")
	if err != nil {
		return nil, false, fmt.Errorf("read clusterpolicy exclude.any: %w", err)
	}
	if !found {
		anyItems = make([]any, 0)
	}

	if hasNamespaceSelectorExclusion(anyItems, policySelectorLabelKey, selectorValue) {
		return policy.DeepCopy(), true, nil
	}

	anyItems = append(anyItems, map[string]any{
		"resources": map[string]any{
			"kinds": []any{"Pod"},
			"namespaceSelector": map[string]any{
				"matchLabels": map[string]any{
					policySelectorLabelKey: selectorValue,
				},
			},
		},
	})

	if excludeMap == nil {
		excludeMap = make(map[string]any)
	}
	excludeMap["any"] = anyItems
	ruleMap["exclude"] = excludeMap
	rules[ruleIndex] = ruleMap

	updated := policy.DeepCopy()
	if err := unstructured.SetNestedSlice(updated.Object, rules, "spec", "rules"); err != nil {
		return nil, false, fmt.Errorf("write clusterpolicy rules: %w", err)
	}
	return updated, false, nil
}

func hasNamespaceSelectorExclusion(items []any, selectorKey string, selectorValue string) bool {
	for _, item := range items {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		matchLabels, found, err := unstructured.NestedStringMap(itemMap, "resources", "namespaceSelector", "matchLabels")
		if err != nil || !found {
			continue
		}
		if strings.TrimSpace(matchLabels[selectorKey]) == selectorValue {
			return true
		}
	}
	return false
}
