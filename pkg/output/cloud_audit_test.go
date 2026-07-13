package output

import "testing"

func TestCloudAuditUserName(t *testing.T) {
	serviceAccount := "system:serviceaccount:kube-system:volcano-controller"
	if got := cloudAuditUserName(serviceAccount, false); got != "volcano-controller" {
		t.Fatalf("compact service account = %q", got)
	}
	if got := cloudAuditUserName(serviceAccount, true); got != serviceAccount {
		t.Fatalf("long service account = %q", got)
	}
	if got := cloudAuditUserName("linzhouhan", false); got != "linzhouhan" {
		t.Fatalf("regular username = %q", got)
	}
}
