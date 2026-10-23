package cmd

import (
	"strings"
	"testing"
)

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
