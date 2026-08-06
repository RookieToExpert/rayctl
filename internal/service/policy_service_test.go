package service

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestSupportedClusterPolicies(t *testing.T) {
	items := SupportedClusterPolicies()
	if len(items) != 9 {
		t.Fatalf("SupportedClusterPolicies() returned %d items, want 9", len(items))
	}
	if items[0].PolicyName != "disallow-capabilities" || items[len(items)-1].PolicyName != "disallow-selinux" {
		t.Fatalf("SupportedClusterPolicies() is not sorted: first=%q last=%q", items[0].PolicyName, items[len(items)-1].PolicyName)
	}
	wantSELinuxRules := []string{"selinux-type", "selinux-user-role"}
	if !reflect.DeepEqual(items[len(items)-1].RuleNames, wantSELinuxRules) {
		t.Fatalf("disallow-selinux rules = %v, want %v", items[len(items)-1].RuleNames, wantSELinuxRules)
	}
}

func TestEnsureClusterPolicyExclusionUpdatesEveryTargetRule(t *testing.T) {
	policy := testClusterPolicy("disallow-selinux", "selinux-type", "selinux-user-role")
	updated, alreadyPresent, err := ensureClusterPolicyExclusion(
		policy,
		[]string{"selinux-type", "selinux-user-role"},
		"vc-019f-test",
	)
	if err != nil {
		t.Fatalf("ensureClusterPolicyExclusion() error = %v", err)
	}
	if alreadyPresent {
		t.Fatal("ensureClusterPolicyExclusion() alreadyPresent = true, want false")
	}

	rules, _, _ := unstructured.NestedSlice(updated.Object, "spec", "rules")
	for _, rule := range rules {
		ruleMap := rule.(map[string]any)
		items, found, err := unstructured.NestedSlice(ruleMap, "exclude", "any")
		if err != nil || !found || !hasClusterPolicyExclusion(items, "vc-019f-test") {
			t.Fatalf("rule %v does not contain the VC exclusion", ruleMap["name"])
		}
	}
}

func TestEnsureClusterPolicyExclusionCompletesPartialUpdate(t *testing.T) {
	policy := testClusterPolicy("disallow-selinux", "selinux-type", "selinux-user-role")
	rules, _, _ := unstructured.NestedSlice(policy.Object, "spec", "rules")
	firstRule := rules[0].(map[string]any)
	firstRule["exclude"] = map[string]any{"any": []any{testVCPolicyExclusion("vc-019f-test")}}
	rules[0] = firstRule
	_ = unstructured.SetNestedSlice(policy.Object, rules, "spec", "rules")

	updated, alreadyPresent, err := ensureClusterPolicyExclusion(
		policy,
		[]string{"selinux-type", "selinux-user-role"},
		"vc-019f-test",
	)
	if err != nil {
		t.Fatalf("ensureClusterPolicyExclusion() error = %v", err)
	}
	if alreadyPresent {
		t.Fatal("partial exclusion must not be reported as already present")
	}

	updatedRules, _, _ := unstructured.NestedSlice(updated.Object, "spec", "rules")
	for _, rule := range updatedRules {
		items, _, _ := unstructured.NestedSlice(rule.(map[string]any), "exclude", "any")
		if len(items) != 1 {
			t.Fatalf("rule %v has %d exclusions, want 1", rule.(map[string]any)["name"], len(items))
		}
	}
}

func TestExtractClusterPolicyWhitelistDeduplicatesRules(t *testing.T) {
	policy := testClusterPolicy("disallow-selinux", "selinux-type", "selinux-user-role")
	rules, _, _ := unstructured.NestedSlice(policy.Object, "spec", "rules")
	for i := range rules {
		rule := rules[i].(map[string]any)
		rule["exclude"] = map[string]any{"any": []any{testVCPolicyExclusion("vc-019f-test")}}
		rules[i] = rule
	}
	_ = unstructured.SetNestedSlice(policy.Object, rules, "spec", "rules")

	items, err := extractClusterPolicyWhitelist(policy, []string{"selinux-type", "selinux-user-role"})
	if err != nil {
		t.Fatalf("extractClusterPolicyWhitelist() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("extractClusterPolicyWhitelist() returned %d items, want 1", len(items))
	}
}

func TestHasClusterPolicyExclusionRecognizesLegacyPodSelector(t *testing.T) {
	items := []any{map[string]any{
		"resources": map[string]any{
			"selector": map[string]any{
				"matchExpressions": []any{map[string]any{
					"key":      policyNamespaceLabelPrefix + "019f-test",
					"operator": "Exists",
				}},
			},
		},
	}}

	if !hasClusterPolicyExclusion(items, "vc-019f-test") {
		t.Fatal("legacy Pod selector exclusion was not recognized")
	}
}

func testClusterPolicy(name string, ruleNames ...string) *unstructured.Unstructured {
	rules := make([]any, 0, len(ruleNames))
	for _, ruleName := range ruleNames {
		rules = append(rules, map[string]any{"name": ruleName})
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kyverno.io/v1",
		"kind":       "ClusterPolicy",
		"metadata":   map[string]any{"name": name},
		"spec":       map[string]any{"rules": rules},
	}}
}

func testVCPolicyExclusion(selectorValue string) map[string]any {
	return map[string]any{
		"resources": map[string]any{
			"kinds": []any{"Pod"},
			"namespaceSelector": map[string]any{
				"matchLabels": map[string]any{policySelectorLabelKey: selectorValue},
			},
		},
	}
}
