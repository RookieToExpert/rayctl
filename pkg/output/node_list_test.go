package output

import (
	"io"
	"os"
	"strings"
	"testing"

	"rayctl/internal/service"
)

func captureNodeListOutput(t *testing.T, long bool) string {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = write
	defer func() {
		os.Stdout = old
		write.Close()
		read.Close()
	}()
	type captured struct {
		data []byte
		err  error
	}
	done := make(chan captured, 1)
	go func() {
		data, err := io.ReadAll(read)
		done <- captured{data, err}
	}()
	PrintNodeList([]service.NodeListItem{{
		Name: "host-1", Ready: "True", Schedulable: "Yes", Repair: true,
		ProdRole: "ecp-private", InternalIP: "10.0.0.1", ClusterName: "vc-demo",
		Model: "910C", QueueName: "queue-demo", Tenant: "tenant-demo",
	}}, "", 1, 1, 1, 100, true, long)
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	return string(result.data)
}

func TestNodeListDefaultColumns(t *testing.T) {
	got := captureNodeListOutput(t, false)
	for _, value := range []string{"HOSTNAME", "RDY", "SCH", "RPR", "PROD", "IP", "CLUSTER"} {
		if !strings.Contains(got, value) {
			t.Fatalf("missing %q in %s", value, got)
		}
	}
	for _, value := range []string{"MODEL", "QUEUE", "TENANT"} {
		if strings.Contains(got, value) {
			t.Fatalf("unexpected %q in default view: %s", value, got)
		}
	}
}

func TestNodeListLongColumns(t *testing.T) {
	got := captureNodeListOutput(t, true)
	for _, value := range []string{"HOSTNAME", "MODEL", "QUEUE", "TENANT", "910C", "queue-demo"} {
		if !strings.Contains(got, value) {
			t.Fatalf("missing %q in %s", value, got)
		}
	}
	for _, value := range []string{"RDY", "SCH", "RPR", "PROD", "CLUSTER"} {
		if strings.Contains(got, value) {
			t.Fatalf("unexpected %q in long view: %s", value, got)
		}
	}
}
