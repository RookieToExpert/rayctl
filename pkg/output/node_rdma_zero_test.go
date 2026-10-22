package output

import (
	"rayctl/internal/service"
	"strings"
	"testing"
)

func TestNodeZeroRDMAShowsZeroAndRedWithoutChangingTableWidth(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NO_COLOR", "")
	details := &service.NodeDescribe{Hostname: "host", RDMAResources: []service.NodeExtendedResource{{Name: "rdma/hca", Capacity: "0", Allocatable: "0", Requested: "0"}}}
	text := captureTableOutput(t, func() { PrintNodeDescribe(details, false, nil) })
	if !strings.Contains(text, "RDMA") || !strings.Contains(text, "\x1b[31m0/0\x1b[0m") {
		t.Fatal(text)
	}
	plain := strings.ReplaceAll(strings.ReplaceAll(text, "\x1b[31m", ""), "\x1b[0m", "")
	t.Setenv("NO_COLOR", "1")
	withoutColor := captureTableOutput(t, func() { PrintNodeDescribe(details, false, nil) })
	if plain != withoutColor {
		t.Fatal("ANSI changed layout")
	}
}
