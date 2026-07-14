package service

import (
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRBACMemberTypeMatches(t *testing.T) {
	tests := []struct {
		actual   string
		expected string
		want     bool
	}{
		{actual: "user", expected: "User", want: true},
		{actual: "GROUP", expected: "Group", want: true},
		{actual: "user_group", expected: "Group", want: true},
		{actual: "user-group", expected: "Group", want: true},
		{actual: "group", expected: "User", want: false},
	}
	for _, test := range tests {
		if got := rbacMemberTypeMatches(test.actual, test.expected); got != test.want {
			t.Fatalf("rbacMemberTypeMatches(%q, %q) = %v, want %v", test.actual, test.expected, got, test.want)
		}
	}
}

func TestGrantBindingMembersSupportsUsersAndGroups(t *testing.T) {
	members := []RBACGrantMember{
		{Type: "User", Name: "test1", ID: "user-id"},
		{Type: "Group", Name: "team1", ID: "group-id"},
	}

	annotations, subjects := grantBindingMembers(members, "default", true)
	if annotations["user/user-id"] != "test1" {
		t.Fatalf("user annotation = %q", annotations["user/user-id"])
	}
	if annotations["group/group-id"] != "team1" {
		t.Fatalf("group annotation = %q", annotations["group/group-id"])
	}
	if len(subjects) != 2 {
		t.Fatalf("subjects length = %d, want 2", len(subjects))
	}
	if subjects[0].Kind != "User" || subjects[0].Name != "user-id" || subjects[0].Namespace != "default" {
		t.Fatalf("unexpected user subject: %#v", subjects[0])
	}
	if subjects[1].Kind != "Group" || subjects[1].Name != "group-id" || subjects[1].Namespace != "default" {
		t.Fatalf("unexpected group subject: %#v", subjects[1])
	}
}

func TestGrantBindingMembersOmitsNamespaceForClusterBinding(t *testing.T) {
	_, subjects := grantBindingMembers([]RBACGrantMember{{Type: "User", Name: "test1", ID: "user-id"}}, "all", false)
	if len(subjects) != 1 || subjects[0].Namespace != "" {
		t.Fatalf("unexpected cluster subject: %#v", subjects)
	}
}

func TestAddRBACSubjectsUsesKindAndID(t *testing.T) {
	got := map[string]struct{}{}
	addRBACSubjects(got, []rbacv1.Subject{
		{Kind: "User", Name: "same-id"},
		{Kind: "Group", Name: "same-id"},
		{Kind: "ServiceAccount", Name: "ignored"},
	})
	if _, ok := got["user/same-id"]; !ok {
		t.Fatal("user subject was not indexed")
	}
	if _, ok := got["group/same-id"]; !ok {
		t.Fatal("group subject was not indexed")
	}
	if len(got) != 2 {
		t.Fatalf("indexed subjects = %d, want 2", len(got))
	}
}

func TestSubjectsTextUsesAnnotationNames(t *testing.T) {
	subjects := []rbacv1.Subject{
		{Kind: "User", Name: "user-id"},
		{Kind: "Group", Name: "group-id"},
		{Kind: "User", Name: "fallback-id"},
	}
	annotations := map[string]string{
		"user/user-id":   "test5",
		"group/group-id": "ug-a3-test",
	}
	if got, want := subjectsText(subjects, annotations), "1. fallback-id\n2. test5\n3. ug-a3-test"; got != want {
		t.Fatalf("subjectsText() = %q, want %q", got, want)
	}
}

func TestSubjectsTextKeepsSingleNameCompact(t *testing.T) {
	subjects := []rbacv1.Subject{{Kind: "User", Name: "user-id"}}
	annotations := map[string]string{"user/user-id": "test5"}
	if got, want := subjectsText(subjects, annotations), "test5"; got != want {
		t.Fatalf("subjectsText() = %q, want %q", got, want)
	}
}

func TestRoleRefTextOnlyReturnsRoleName(t *testing.T) {
	role := rbacv1.RoleRef{Kind: "ClusterRole", Name: "cluster-admin"}
	if got := roleRefText(role); got != "cluster-admin" {
		t.Fatalf("roleRefText() = %q", got)
	}
}

func TestFindRoleBindingByNameRequiresExactMatch(t *testing.T) {
	bindings := []rbacv1.RoleBinding{
		{ObjectMeta: metav1.ObjectMeta{Name: "edit-abc"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "edit-def"}},
	}
	binding, ok := findRoleBindingByName(bindings, "edit-def")
	if !ok || binding.Name != "edit-def" {
		t.Fatalf("findRoleBindingByName() = %#v, %v", binding, ok)
	}
	if _, ok := findRoleBindingByName(bindings, "edit"); ok {
		t.Fatal("partial binding name unexpectedly matched")
	}
}

func TestFindClusterRoleBindingByNameRequiresExactMatch(t *testing.T) {
	bindings := []rbacv1.ClusterRoleBinding{
		{ObjectMeta: metav1.ObjectMeta{Name: "cluster-admin-abc"}},
	}
	binding, ok := findClusterRoleBindingByName(bindings, "cluster-admin-abc")
	if !ok || binding.Name != "cluster-admin-abc" {
		t.Fatalf("findClusterRoleBindingByName() = %#v, %v", binding, ok)
	}
}
