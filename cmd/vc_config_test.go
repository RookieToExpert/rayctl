package cmd

import (
	"context"
	"rayctl/internal/service"
	"strings"
	"testing"
)

func TestVCOverviewDcloudStillAllowsHCMappingWithoutFlavorAPI(t *testing.T) {
	detail := &service.VCDetailResult{Name: "vc-cloud", UID: "uid-cloud"}
	enrichVCControlPlane(context.Background(), detail, service.HCEnvironment{Name: "dcloud"})
	if detail.ControlPlane == nil || detail.ControlPlane.Desired != nil || detail.ControlPlane.AppliedFlavor != "" || len(detail.ControlPlane.Warnings) != 1 || !strings.Contains(detail.ControlPlane.Warnings[0], "未采集") {
		t.Fatalf("config=%+v", detail.ControlPlane)
	}
}

func TestVCConfigUpdateRejectsInvalidOptionsBeforePlatformAccess(t *testing.T) {
	for _, test := range []struct {
		args    []string
		message string
	}{
		{[]string{"vc-demo", "--flavor", "huge"}, "无效控制面规格"},
		{[]string{"vc-demo", "--flavor", "unlimit"}, "--allow-unlimit"},
		{[]string{"vc-demo", "--flavor", "medium", "--timeout", "0s"}, "--timeout 必须大于 0"},
	} {
		command := newVCConfigUpdateCmd()
		command.SetArgs(test.args)
		if err := command.Execute(); err == nil || !strings.Contains(err.Error(), test.message) {
			t.Fatalf("args=%v err=%v", test.args, err)
		}
	}
}
