package output

import (
	"strings"
	"testing"

	"rayctl/internal/service"
)

func rbacSampleResult() *service.RBACGetResult {
	return &service.RBACGetResult{
		ClusterName: "vc-muxi-h3c-dlmuxi",
		Items: []service.RBACBindingItem{
			{Kind: "CRB", Namespace: "all", Name: "cluster-admin-d4crd", Role: "cluster-admin", Subjects: "1. hubingying\n2. peizhilin", CreatedAt: "2026-10-14 18:41:57"},
			{Kind: "RB", Namespace: "default", Name: "edit-2jh7g", Role: "edit", Subjects: "zhangpeijie.p", CreatedAt: "2026-10-30 11:07:16"},
		},
	}
}

func headerLine(out string, first string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, first) {
			return line
		}
	}
	return ""
}

func TestRBACSummaryDefaultIncludesBinding(t *testing.T) {
	out := captureTableOutput(t, func() { PrintRBACGetSummary([]*service.RBACGetResult{rbacSampleResult()}, false, false) })
	t.Log("\n" + out)
	h := headerLine(out, "NAMESPACE")
	iN, iT, iB, iR, iS := strings.Index(h, "NAMESPACE"), strings.Index(h, "TYPE"), strings.Index(h, "BINDING"), strings.Index(h, "ROLE"), strings.Index(h, "SUBJECTS")
	if !(iN >= 0 && iN < iT && iT < iB && iB < iR && iR < iS) {
		t.Fatalf("unexpected header order: %q", h)
	}
	for _, want := range []string{"cluster-admin-d4crd", "edit-2jh7g", "CRB", "RB"} {
		if !strings.Contains(out, want) {
			t.Fatalf("default output missing binding %q:\n%s", want, out)
		}
	}
}

func TestRBACLongOmitsBinding(t *testing.T) {
	out := captureTableOutput(t, func() { PrintRBACGetSummary([]*service.RBACGetResult{rbacSampleResult()}, false, true) })
	t.Log("\n" + out)
	h := headerLine(out, "NAMESPACE")
	if strings.Contains(h, "BINDING") || !strings.Contains(h, "TYPE") || !strings.Contains(h, "CREATE TIME") {
		t.Fatalf("unexpected -l header: %q", h)
	}
	if strings.Contains(out, "cluster-admin-d4crd") || strings.Contains(out, "edit-2jh7g") {
		t.Fatalf("-l output should not include binding names:\n%s", out)
	}
}
