package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"rayctl/internal/service"
)

func listSummaryFixture(typ reflect.Type, n int, name string) reflect.Value {
	if typ.Kind() == reflect.Pointer {
		v := reflect.New(typ.Elem())
		if f := v.Elem().FieldByName("Items"); f.IsValid() && f.CanSet() && f.Kind() == reflect.Slice {
			f.Set(reflect.MakeSlice(f.Type(), n, n))
		}
		if f := v.Elem().FieldByName("Total"); f.IsValid() && f.CanSet() && f.Kind() >= reflect.Int && f.Kind() <= reflect.Int64 {
			f.SetInt(100)
		}
		if f := v.Elem().FieldByName("AID"); name == "aid list" && f.IsValid() && f.CanSet() {
			f.SetBool(true)
		}
		return v
	}
	if typ.Kind() == reflect.Slice {
		if typ.Elem().Kind() == reflect.Pointer {
			v := reflect.MakeSlice(typ, 1, 1)
			v.Index(0).Set(listSummaryFixture(typ.Elem(), n, name))
			return v
		}
		return reflect.MakeSlice(typ, n, n)
	}
	panic("unsupported audit argument " + typ.String())
}
func TestAllListPrintersReportActualRows(t *testing.T) {
	cases := []struct {
		name string
		fn   any
		long bool
	}{
		{"afs list", PrintAFSList, true}, {"aid list", PrintSSPAIDList, true}, {"ait list", PrintSSPAITList, false},
		{"ate list", PrintECPJobList, false}, {"ate list cluster", PrintJobClusterList, false},
		{"cluster list", PrintSSPClusterList, false}, {"ecs list", PrintECSList, true}, {"group list", PrintGroupList, true},
		{"natgw list", PrintNATGatewayList, true}, {"policy list", PrintSupportedClusterPolicies, false},
		{"queue list", PrintSSPQueueList, false}, {"subnet list", PrintSubnetList, true}, {"user list", PrintUserList, true},
		{"vc list", PrintVCList, false}, {"vpc list", PrintVPCList, true}, {"workspace list", PrintSSPWorkspaceList, false},
		{"air gateway list", PrintSSPAIRGatewayList, false}, {"air job list", PrintSSPAIRJobList, true},
		{"queue node list", PrintSSPQueueNodeList, true}, {"queue workload list", PrintSSPQueueWorkloads, false},
		{"vc node list", PrintVCNodeList, true}, {"vc node list (multi)", PrintVCNodeListMany, true},
		{"vc workload list", PrintVCWorkloads, false}, {"workspace workload list", PrintSSPWorkspaceWorkloads, false},
	}
	emit := func(name string, n int, long bool, render func()) {
		text := captureListTableOutput(t, render)
		lines := strings.Split(strings.TrimSpace(text), "\n")
		last := ""
		if len(lines) > 0 {
			last = lines[len(lines)-1]
		}
		want := fmt.Sprintf("本次输出 %d 条。", n)
		if last != want || strings.Count(text, "本次输出 ") != 1 {
			t.Fatalf("%s n=%d long=%v: want one final %q:\n%s", name, n, long, want, text)
		}
	}
	for _, c := range cases {
		modes := []bool{false}
		if c.long {
			modes = append(modes, true)
		}
		for _, wide := range modes {
			for _, n := range []int{0, 2} {
				emit(c.name, n, wide, func() {
					fn := reflect.ValueOf(c.fn)
					arg := listSummaryFixture(fn.Type().In(0), n, c.name)
					args := []reflect.Value{arg}
					if fn.Type().NumIn() > 1 {
						args = append(args, reflect.ValueOf(wide))
					}
					fn.Call(args)
				})
			}
		}
	}
	for _, wide := range []bool{false, true} {
		for _, n := range []int{0, 2} {
			emit("node list", n, wide, func() { PrintNodeList(make([]service.NodeListItem, n), "", n, 1, n, 100, true, wide) })
		}
	}
	emit("ate list (warnings)", 2, false, func() {
		v := listSummaryFixture(reflect.TypeOf((*service.JobClusterListResult)(nil)), 2, "ate list")
		v.Elem().FieldByName("Warnings").Set(reflect.ValueOf([]string{"synthetic query warning"}))
		reflect.ValueOf(PrintECPJobList).Call([]reflect.Value{v})
	})
}

func captureListTableOutput(t *testing.T, render func()) string {
	t.Helper()
	return captureTableOutput(t, func() {
		if err := WithListSummary(os.Stdout, func() error { render(); return nil }); err != nil {
			t.Fatal(err)
		}
	})
}
func TestListSummaryCombinesScopesAndExcludesPlaceholders(t *testing.T) {
	text := captureListTableOutput(t, func() {
		PrintSSPQueueNodeList(&service.SSPQueueNodeListResult{Items: make([]service.VCNodeListItem, 2)}, false)
		PrintSSPQueueNodeList(&service.SSPQueueNodeListResult{}, false)
		PrintSSPQueueNodeList(nil, false)
		PrintSSPQueueNodeList(&service.SSPQueueNodeListResult{Items: make([]service.VCNodeListItem, 1)}, false)
	})
	if !strings.HasSuffix(strings.TrimSpace(text), "本次输出 3 条。") || strings.Count(text, "本次输出 ") != 1 {
		t.Fatal(text)
	}
	text = captureListTableOutput(t, func() {
		PrintVCWorkloads([]*service.VCWorkloadResult{{Items: make([]service.SSPQueueWorkloadItem, 2)}, nil, {}, {Items: make([]service.SSPQueueWorkloadItem, 1)}})
	})
	if !strings.HasSuffix(strings.TrimSpace(text), "本次输出 3 条。") {
		t.Fatal(text)
	}
}
func TestListSummaryCountsLimitRatherThanMatchingTotal(t *testing.T) {
	text := captureListTableOutput(t, func() { PrintNodeList(make([]service.NodeListItem, 2), "", 100, 1, 2, 2, true, false) })
	if !strings.Contains(text, "匹配共 100 个节点") || !strings.HasSuffix(strings.TrimSpace(text), "本次输出 2 条。") {
		t.Fatal(text)
	}
	text = captureListTableOutput(t, func() {
		PrintECSList(&service.ECSListResult{Items: make([]service.ECSListItem, 2), Total: 100, Truncated: true}, true)
	})
	if !strings.Contains(text, "匹配共 100 条") || !strings.HasSuffix(strings.TrimSpace(text), "本次输出 2 条。") {
		t.Fatal(text)
	}
}
func TestListSummaryPreservesFailureAndIncompleteResults(t *testing.T) {
	failure := errors.New("lookup denied")
	for _, rendered := range []bool{false, true} {
		text := captureTableOutput(t, func() {
			err := WithListSummary(os.Stdout, func() error {
				if rendered {
					PrintSSPQueueList(&service.SSPQueueListResult{Items: make([]service.SSPQueueItem, 2)})
				}
				return failure
			})
			if !errors.Is(err, failure) {
				t.Fatal(err)
			}
		})
		if rendered {
			if !strings.HasSuffix(strings.TrimSpace(text), "本次输出 2 条（查询未完整完成）。") {
				t.Fatal(text)
			}
		} else if strings.Contains(text, "本次输出") {
			t.Fatal(text)
		}
	}
	text := captureListTableOutput(t, func() {
		PrintSSPAIRJobList(&service.SSPAIRJobListResult{QueryIncomplete: true, Items: make([]service.SSPAIRJobItem, 2)}, false)
	})
	if !strings.HasSuffix(strings.TrimSpace(text), "本次输出 2 条（查询未完整完成）。") {
		t.Fatal(text)
	}
	// A completed empty list after an error must not inherit the earlier count.
	text = captureListTableOutput(t, func() { PrintSSPQueueList(&service.SSPQueueListResult{}) })
	if !strings.HasSuffix(strings.TrimSpace(text), "本次输出 0 条。") {
		t.Fatal(text)
	}
}
func TestListSummaryLeavesJSONUnchanged(t *testing.T) {
	render := func() {
		PrintSSPAIRJobList(&service.SSPAIRJobListResult{Items: make([]service.SSPAIRJobItem, 2), QueryIncomplete: true, Warnings: []string{"partial lookup"}}, true)
	}
	encode := func(wrapped bool) string {
		session := BeginJSON()
		defer EndJSON()
		var buffer bytes.Buffer
		if wrapped {
			if err := WithListSummary(&buffer, func() error { render(); return nil }); err != nil {
				t.Fatal(err)
			}
		} else {
			render()
		}
		if err := session.Write(&buffer, true, 2, nil); err != nil {
			t.Fatal(err)
		}
		return buffer.String()
	}
	before, after := encode(false), encode(true)
	if before != after || strings.Contains(after, "本次输出") {
		t.Fatal(after)
	}
	var payload struct {
		Items   []any
		Summary struct {
			Returned int
			Complete bool
		}
	}
	if err := json.Unmarshal([]byte(after), &payload); err != nil || len(payload.Items) != 2 || payload.Summary.Returned != 2 || payload.Summary.Complete {
		t.Fatal(after, err)
	}
}
func TestListSummaryNestedRenderEmitsOnce(t *testing.T) {
	text := captureListTableOutput(t, func() {
		PrintSSPQueueList(&service.SSPQueueListResult{Items: make([]service.SSPQueueItem, 1)})
		_ = WithListSummary(os.Stdout, func() error {
			PrintSSPQueueList(&service.SSPQueueListResult{Items: make([]service.SSPQueueItem, 2)})
			return nil
		})
	})
	if strings.Count(text, "本次输出 ") != 1 || !strings.HasSuffix(strings.TrimSpace(text), "本次输出 3 条。") {
		t.Fatal(text)
	}
}
