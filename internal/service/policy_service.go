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
	policySelectorLabelKey          = "vcluster.loft.sh/vcluster-name"
	policySelectorNamespaceLabelKey = "vcluster.loft.sh/vcluster-namespace"
	policyLegacySelectorLabelKey    = "cluster.x-k8s.io/vcluster-name"
	policyNamespaceLabelPrefix      = "vcluster.loft.sh/ns-label-vc-"
)

var supportedClusterPolicyRules = map[string][]string{
	"disallow-capabilities":          {"adding-capabilities"},
	"disallow-host-namespaces":       {"host-namespaces"},
	"disallow-host-path":             {"host-path"},
	"disallow-host-ports":            {"host-ports-none"},
	"disallow-host-ports-range":      {"host-port-range"},
	"disallow-host-process":          {"host-process-containers"},
	"disallow-privileged-containers": {"privileged-containers"},
	"disallow-proc-mount":            {"check-proc-mount"},
	"disallow-selinux":               {"selinux-type", "selinux-user-role"},
}

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

type PolicyGetQueryResult struct {
	Identifier string
	Result     *PolicyGetResult
	Err        error
}

type PolicyWhitelistItem struct {
	ClusterName   string
	ClusterUID    string
	Tenant        string
	SelectorKey   string
	SelectorValue string
}

type SupportedPolicyItem struct {
	PolicyName string
	RuleNames  []string
}

func SupportedClusterPolicies() []SupportedPolicyItem {
	names := supportedClusterPolicyNames()
	items := make([]SupportedPolicyItem, 0, len(names))
	for _, name := range names {
		items = append(items, SupportedPolicyItem{
			PolicyName: name,
			RuleNames:  append([]string(nil), supportedClusterPolicyRules[name]...),
		})
	}
	return items
}

func NewPolicyService(dynamicClient dynamic.Interface, clusterService *ClusterService) *PolicyService {
	return &PolicyService{
		dynamicClient:  dynamicClient,
		clusterService: clusterService,
	}
}

func (s *PolicyService) UpdateClusterPolicy(ctx context.Context, policyName string, clusterIdentifier string) (*PolicyUpdateResult, error) {
	policyName = strings.TrimSpace(policyName)
	ruleNames, ok := supportedClusterPolicyRules[policyName]
	if !ok {
		return nil, unsupportedClusterPolicyError("update", policyName)
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

	updatedObj, alreadyPresent, err := ensureClusterPolicyExclusion(policy, ruleNames, selectorValue)
	if err != nil {
		return nil, err
	}

	result := &PolicyUpdateResult{
		PolicyName:     policyName,
		ClusterName:    clusterResult.ClusterName,
		ClusterUID:     clusterResult.ClusterUID,
		RuleName:       strings.Join(ruleNames, ", "),
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
	ruleNames, ok := supportedClusterPolicyRules[policyName]
	if !ok {
		return nil, unsupportedClusterPolicyError("get", policyName)
	}
	if s.dynamicClient == nil {
		return nil, fmt.Errorf("policy get requires kubernetes dynamic client")
	}

	policy, err := s.dynamicClient.Resource(clusterPolicyGVR).Get(ctx, policyName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get clusterpolicy %q: %w", policyName, err)
	}

	items, err := extractClusterPolicyWhitelist(policy, ruleNames)
	if err != nil {
		return nil, err
	}
	result := &PolicyGetResult{
		PolicyName: policyName,
		Items:      items,
	}

	clusterIdentifier = strings.TrimSpace(clusterIdentifier)
	if clusterIdentifier == "" {
		s.resolvePolicyWhitelistNames(ctx, items)
		sortPolicyWhitelistItems(items)
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

func (s *PolicyService) GetClusterPolicyMany(ctx context.Context, policyName string, clusterIdentifiers []string, maxParallel int) []PolicyGetQueryResult {
	policyName = strings.TrimSpace(policyName)
	ruleNames, ok := supportedClusterPolicyRules[policyName]
	if !ok {
		err := unsupportedClusterPolicyError("get", policyName)
		return policyQueryErrors(clusterIdentifiers, err)
	}
	if s.dynamicClient == nil {
		return policyQueryErrors(clusterIdentifiers, fmt.Errorf("policy get requires kubernetes dynamic client"))
	}
	if s.clusterService == nil {
		return policyQueryErrors(clusterIdentifiers, fmt.Errorf("policy get requires cluster service when filtering by vc"))
	}

	policy, err := s.dynamicClient.Resource(clusterPolicyGVR).Get(ctx, policyName, metav1.GetOptions{})
	if err != nil {
		return policyQueryErrors(clusterIdentifiers, fmt.Errorf("get clusterpolicy %q: %w", policyName, err))
	}
	items, err := extractClusterPolicyWhitelist(policy, ruleNames)
	if err != nil {
		return policyQueryErrors(clusterIdentifiers, err)
	}

	return boundedMap(ctx, clusterIdentifiers, maxParallel, func(queryCtx context.Context, identifier string) PolicyGetQueryResult {
		query := PolicyGetQueryResult{Identifier: identifier}
		clusterResult, resolveErr := s.clusterService.Get(queryCtx, identifier)
		if resolveErr != nil {
			query.Err = resolveErr
			return query
		}
		controlPlaneNamespace := strings.TrimSpace(clusterResult.ControlPlaneNamespace)
		result := &PolicyGetResult{
			PolicyName:     policyName,
			TargetCluster:  clusterResult.ClusterName,
			TargetUID:      clusterResult.ClusterUID,
			TargetSelector: policySelectorLabelKey + "=" + controlPlaneNamespace,
		}
		matches := make([]PolicyWhitelistItem, 0)
		for _, item := range items {
			if item.ClusterUID != clusterResult.ClusterUID {
				continue
			}
			result.Matched = true
			matches = append(matches, item)
		}
		if len(matches) == 0 {
			matches = append(matches, PolicyWhitelistItem{ClusterName: clusterResult.ClusterName, ClusterUID: clusterResult.ClusterUID, SelectorKey: policySelectorLabelKey, SelectorValue: controlPlaneNamespace})
		}
		result.Items = matches
		query.Result = result
		return query
	})
}

func policyQueryErrors(identifiers []string, err error) []PolicyGetQueryResult {
	results := make([]PolicyGetQueryResult, len(identifiers))
	for index, identifier := range identifiers {
		results[index] = PolicyGetQueryResult{Identifier: identifier, Err: err}
	}
	return results
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

func supportedClusterPolicyNames() []string {
	names := make([]string, 0, len(supportedClusterPolicyRules))
	for name := range supportedClusterPolicyRules {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func unsupportedClusterPolicyError(operation string, policyName string) error {
	return fmt.Errorf("policy %s 不支持 %q；当前支持: %s", operation, policyName, strings.Join(supportedClusterPolicyNames(), ", "))
}

func ensureClusterPolicyExclusion(policy *unstructured.Unstructured, ruleNames []string, selectorValue string) (*unstructured.Unstructured, bool, error) {
	if policy == nil {
		return nil, false, fmt.Errorf("clusterpolicy is required")
	}
	if len(ruleNames) == 0 {
		return nil, false, fmt.Errorf("clusterpolicy %q has no configured target rules", policy.GetName())
	}

	rules, found, err := unstructured.NestedSlice(policy.Object, "spec", "rules")
	if err != nil {
		return nil, false, fmt.Errorf("read clusterpolicy rules: %w", err)
	}
	if !found || len(rules) == 0 {
		return nil, false, fmt.Errorf("clusterpolicy %q has no spec.rules", policy.GetName())
	}

	ruleIndexes := make(map[string]int, len(ruleNames))
	for i, item := range rules {
		ruleMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(fmt.Sprintf("%v", ruleMap["name"]))
		for _, target := range ruleNames {
			if name == target {
				ruleIndexes[target] = i
				break
			}
		}
	}
	for _, ruleName := range ruleNames {
		if _, found := ruleIndexes[ruleName]; !found {
			return nil, false, fmt.Errorf("clusterpolicy %q has no rule named %q", policy.GetName(), ruleName)
		}
	}

	updatedAnyRule := false
	for _, ruleName := range ruleNames {
		ruleIndex := ruleIndexes[ruleName]
		ruleMap, ok := rules[ruleIndex].(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("clusterpolicy rule %q has unexpected format", ruleName)
		}

		excludeMap, _, err := unstructured.NestedMap(ruleMap, "exclude")
		if err != nil {
			return nil, false, fmt.Errorf("read clusterpolicy rule %q exclude block: %w", ruleName, err)
		}
		anyItems, found, err := unstructured.NestedSlice(ruleMap, "exclude", "any")
		if err != nil {
			return nil, false, fmt.Errorf("read clusterpolicy rule %q exclude.any: %w", ruleName, err)
		}
		if !found {
			anyItems = make([]any, 0)
		}

		if hasClusterPolicyExclusion(anyItems, selectorValue) {
			continue
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
		updatedAnyRule = true
	}

	if !updatedAnyRule {
		return policy.DeepCopy(), true, nil
	}

	updated := policy.DeepCopy()
	if err := unstructured.SetNestedSlice(updated.Object, rules, "spec", "rules"); err != nil {
		return nil, false, fmt.Errorf("write clusterpolicy rules: %w", err)
	}
	return updated, false, nil
}

func hasClusterPolicyExclusion(items []any, selectorValue string) bool {
	uid := uidFromPolicySelectorValue(selectorValue)
	for _, item := range items {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		matchLabels, found, err := unstructured.NestedStringMap(itemMap, "resources", "namespaceSelector", "matchLabels")
		if err == nil && found {
			for _, selectorKey := range []string{policySelectorLabelKey, policySelectorNamespaceLabelKey, policyLegacySelectorLabelKey} {
				if strings.TrimSpace(matchLabels[selectorKey]) == selectorValue {
					return true
				}
			}
		}

		matchExpressions, found, err := unstructured.NestedSlice(itemMap, "resources", "selector", "matchExpressions")
		if err != nil || !found {
			continue
		}
		for _, expression := range matchExpressions {
			expressionMap, ok := expression.(map[string]any)
			if !ok {
				continue
			}
			key := strings.TrimSpace(fmt.Sprintf("%v", expressionMap["key"]))
			if uid != "" && key == policyNamespaceLabelPrefix+uid {
				return true
			}
		}
	}
	return false
}

func extractClusterPolicyWhitelist(policy *unstructured.Unstructured, ruleNames []string) ([]PolicyWhitelistItem, error) {
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

	targetRules := make(map[string]struct{}, len(ruleNames))
	for _, ruleName := range ruleNames {
		targetRules[ruleName] = struct{}{}
	}
	foundRules := make(map[string]struct{}, len(ruleNames))
	result := make([]PolicyWhitelistItem, 0)
	seen := make(map[string]struct{})
	for _, item := range rules {
		current, ok := item.(map[string]any)
		if !ok {
			continue
		}
		ruleName := strings.TrimSpace(fmt.Sprintf("%v", current["name"]))
		if _, wanted := targetRules[ruleName]; !wanted {
			continue
		}
		foundRules[ruleName] = struct{}{}
		anyItems, found, err := unstructured.NestedSlice(current, "exclude", "any")
		if err != nil {
			return nil, fmt.Errorf("read clusterpolicy rule %q exclude.any: %w", ruleName, err)
		}
		if !found {
			continue
		}
		extractClusterPolicyWhitelistItems(anyItems, &result, seen)
	}
	for _, ruleName := range ruleNames {
		if _, found := foundRules[ruleName]; !found {
			return nil, fmt.Errorf("clusterpolicy %q has no rule named %q", policy.GetName(), ruleName)
		}
	}
	return result, nil
}

func extractClusterPolicyWhitelistItems(anyItems []any, result *[]PolicyWhitelistItem, seen map[string]struct{}) {
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
				appendPolicyWhitelistItem(result, seen, PolicyWhitelistItem{
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
			appendPolicyWhitelistItem(result, seen, PolicyWhitelistItem{
				ClusterUID:    uid,
				SelectorKey:   selectorKey,
				SelectorValue: operator,
			})
		}
	}
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
	if s == nil || s.clusterService == nil || len(items) == 0 {
		return
	}
	uids := make([]string, 0, len(items))
	for _, item := range items {
		if item.ClusterUID != "" {
			uids = append(uids, item.ClusterUID)
		}
	}
	names, tenants, err := s.clusterService.ResolveDisplayNamesWithProfiles(ctx, uids)
	if err != nil {
		return
	}
	for i := range items {
		uid := items[i].ClusterUID
		items[i].ClusterName = firstNonEmpty(names[uid], "vc-"+uid)
		items[i].Tenant = tenants[uid]
	}
}
