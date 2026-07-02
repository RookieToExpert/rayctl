package service

import (
	"context"
	"fmt"
	"sort"
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
	policySelectorNamespaceLabelKey        = "vcluster.loft.sh/vcluster-namespace"
	policyLegacySelectorLabelKey           = "cluster.x-k8s.io/vcluster-name"
	policyNamespaceLabelPrefix             = "vcluster.loft.sh/ns-label-vc-"
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

type PolicyGetResult struct {
	PolicyName     string
	TargetCluster  string
	TargetUID      string
	TargetSelector string
	Matched        bool
	Items          []PolicyWhitelistItem
}

type PolicyWhitelistItem struct {
	ClusterName   string
	ClusterUID    string
	Tenant        string
	SelectorKey   string
	SelectorValue string
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

func (s *PolicyService) GetClusterPolicy(ctx context.Context, policyName string, clusterIdentifier string) (*PolicyGetResult, error) {
	policyName = strings.TrimSpace(policyName)
	if policyName != disallowPrivilegedContainersPolicyName {
		return nil, fmt.Errorf("当前 policy get 仅支持 %q", disallowPrivilegedContainersPolicyName)
	}
	if s.dynamicClient == nil {
		return nil, fmt.Errorf("policy get requires kubernetes dynamic client")
	}

	policy, err := s.dynamicClient.Resource(clusterPolicyGVR).Get(ctx, policyName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get clusterpolicy %q: %w", policyName, err)
	}

	items, err := extractDisallowPrivilegedWhitelist(policy)
	if err != nil {
		return nil, err
	}
	s.resolvePolicyWhitelistNames(ctx, items)
	sortPolicyWhitelistItems(items)

	result := &PolicyGetResult{
		PolicyName: policyName,
		Items:      items,
	}

	clusterIdentifier = strings.TrimSpace(clusterIdentifier)
	if clusterIdentifier == "" {
		return result, nil
	}
	if s.clusterService == nil {
		return nil, fmt.Errorf("policy get requires cluster service when filtering by vc")
	}

	clusterResult, err := s.clusterService.Get(ctx, clusterIdentifier)
	if err != nil {
		return nil, err
	}

	controlPlaneNamespace := strings.TrimSpace(clusterResult.ControlPlaneNamespace)
	result.TargetCluster = clusterResult.ClusterName
	result.TargetUID = clusterResult.ClusterUID
	result.TargetSelector = policySelectorLabelKey + "=" + controlPlaneNamespace

	matches := make([]PolicyWhitelistItem, 0)
	for _, item := range items {
		if item.ClusterUID != clusterResult.ClusterUID {
			continue
		}
		result.Matched = true
		matches = append(matches, item)
	}
	if len(matches) == 0 {
		matches = append(matches, PolicyWhitelistItem{
			ClusterName:   clusterResult.ClusterName,
			ClusterUID:    clusterResult.ClusterUID,
			SelectorKey:   policySelectorLabelKey,
			SelectorValue: controlPlaneNamespace,
		})
	}
	result.Items = matches
	return result, nil
}

func sortPolicyWhitelistItems(items []PolicyWhitelistItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].ClusterName != items[j].ClusterName {
			return items[i].ClusterName < items[j].ClusterName
		}
		if items[i].ClusterUID != items[j].ClusterUID {
			return items[i].ClusterUID < items[j].ClusterUID
		}
		if items[i].SelectorKey != items[j].SelectorKey {
			return items[i].SelectorKey < items[j].SelectorKey
		}
		return items[i].SelectorValue < items[j].SelectorValue
	})
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

func extractDisallowPrivilegedWhitelist(policy *unstructured.Unstructured) ([]PolicyWhitelistItem, error) {
	if policy == nil {
		return nil, fmt.Errorf("clusterpolicy is required")
	}

	rules, found, err := unstructured.NestedSlice(policy.Object, "spec", "rules")
	if err != nil {
		return nil, fmt.Errorf("read clusterpolicy rules: %w", err)
	}
	if !found || len(rules) == 0 {
		return nil, fmt.Errorf("clusterpolicy %q has no spec.rules", policy.GetName())
	}

	var ruleMap map[string]any
	for _, item := range rules {
		current, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(fmt.Sprintf("%v", current["name"])) == disallowPrivilegedRuleName {
			ruleMap = current
			break
		}
	}
	if ruleMap == nil {
		return nil, fmt.Errorf("clusterpolicy %q has no rule named %q", policy.GetName(), disallowPrivilegedRuleName)
	}

	anyItems, found, err := unstructured.NestedSlice(ruleMap, "exclude", "any")
	if err != nil {
		return nil, fmt.Errorf("read clusterpolicy exclude.any: %w", err)
	}
	if !found {
		return nil, nil
	}

	result := make([]PolicyWhitelistItem, 0)
	seen := make(map[string]struct{})
	for _, item := range anyItems {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}

		matchLabels, found, err := unstructured.NestedStringMap(itemMap, "resources", "namespaceSelector", "matchLabels")
		if err == nil && found {
			for _, selectorKey := range []string{policySelectorLabelKey, policySelectorNamespaceLabelKey, policyLegacySelectorLabelKey} {
				selectorValue := strings.TrimSpace(matchLabels[selectorKey])
				uid := uidFromPolicySelectorValue(selectorValue)
				if uid == "" {
					continue
				}
				appendPolicyWhitelistItem(&result, seen, PolicyWhitelistItem{
					ClusterUID:    uid,
					SelectorKey:   selectorKey,
					SelectorValue: selectorValue,
				})
			}
		}

		matchExpressions, found, err := unstructured.NestedSlice(itemMap, "resources", "selector", "matchExpressions")
		if err != nil || !found {
			continue
		}
		for _, expr := range matchExpressions {
			exprMap, ok := expr.(map[string]any)
			if !ok {
				continue
			}
			selectorKey := strings.TrimSpace(fmt.Sprintf("%v", exprMap["key"]))
			if !strings.HasPrefix(selectorKey, policyNamespaceLabelPrefix) {
				continue
			}
			uid := strings.TrimPrefix(selectorKey, policyNamespaceLabelPrefix)
			if uid == "" {
				continue
			}
			operator := strings.TrimSpace(fmt.Sprintf("%v", exprMap["operator"]))
			if operator == "" || operator == "<nil>" {
				operator = "Exists"
			}
			appendPolicyWhitelistItem(&result, seen, PolicyWhitelistItem{
				ClusterUID:    uid,
				SelectorKey:   selectorKey,
				SelectorValue: operator,
			})
		}
	}
	return result, nil
}

func appendPolicyWhitelistItem(result *[]PolicyWhitelistItem, seen map[string]struct{}, item PolicyWhitelistItem) {
	key := item.ClusterUID + "|" + item.SelectorKey + "|" + item.SelectorValue
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*result = append(*result, item)
}

func uidFromPolicySelectorValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.TrimPrefix(value, "vc-")
}

func (s *PolicyService) resolvePolicyWhitelistNames(ctx context.Context, items []PolicyWhitelistItem) {
	if s == nil || s.clusterService == nil || s.clusterService.vcClient == nil || len(items) == 0 {
		return
	}
	uids := make([]string, 0, len(items))
	for _, item := range items {
		if item.ClusterUID != "" {
			uids = append(uids, item.ClusterUID)
		}
	}
	names, tenants, err := s.clusterService.vcClient.ResolveDisplayNamesWithProfiles(ctx, uids)
	if err != nil {
		return
	}
	for i := range items {
		uid := items[i].ClusterUID
		items[i].ClusterName = firstNonEmpty(names[uid], "vc-"+uid)
		items[i].Tenant = tenants[uid]
	}
}
