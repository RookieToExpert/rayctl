package output

import "testing"

func TestRBACCreateDate(t *testing.T) {
	if got := rbacCreateDate("2026-07-14 13:33:26"); got != "2026-07-14" {
		t.Fatalf("rbacCreateDate() = %q", got)
	}
	if got := rbacCreateDate(""); got != "-" {
		t.Fatalf("rbacCreateDate(empty) = %q", got)
	}
}

func TestWrapCellNoWrapPreservesExplicitLines(t *testing.T) {
	lines := wrapCell("cluster-admin-49glr\ncluster-admin-very-long-name", 20, true)
	want := []string{"cluster-admin-49glr", "cluster-admin-ver..."}
	if len(lines) != len(want) {
		t.Fatalf("line count = %d, want %d", len(lines), len(want))
	}
	for index := range want {
		if lines[index] != want[index] {
			t.Fatalf("lines[%d] = %q, want %q", index, lines[index], want[index])
		}
	}
}
