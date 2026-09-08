package output

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"rayctl/internal/nodehealth"
)

func PrintNodeHealth(result *nodehealth.Result, format string) error {
	if result == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "table":
		printNodeHealthTable(result)
		return nil
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		encoder.SetEscapeHTML(false)
		return encoder.Encode(result)
	default:
		return fmt.Errorf("unsupported --output %q; expected table or json", format)
	}
}

func printNodeHealthTable(result *nodehealth.Result) {
	batch := result.Batch || len(result.Nodes) > 1
	printed := 0
	for _, node := range result.Nodes {
		if batch && node.Status != nodehealth.StatusWarn && node.Status != nodehealth.StatusFail {
			continue
		}
		if printed > 0 {
			fmt.Fprintln(os.Stdout)
		}
		fmt.Fprintf(
			os.Stdout,
			"%s   %s   %s   %s\n\n",
			node.Node,
			emptyDash(node.Ready),
			emptyDash(node.Labels.VC),
			emptyDash(node.Labels.MachineType),
		)
		checks := append([]nodehealth.CheckResult(nil), node.Checks...)
		sort.SliceStable(checks, func(i, j int) bool {
			return healthStatusOrder(checks[i].Status) > healthStatusOrder(checks[j].Status)
		})
		rows := make([][]string, 0, len(checks))
		for _, check := range checks {
			if batch && check.Status != nodehealth.StatusWarn && check.Status != nodehealth.StatusFail {
				continue
			}
			rows = append(rows, []string{string(check.Status), check.ID, emptyDash(check.Message)})
		}
		printBoxTableWithOptions(
			[]string{"STATUS", "CHECK", "EVIDENCE"},
			rows,
			[]int{6, 18, 112},
			tableOptions{minWidths: []int{6, 14, 32}},
		)
		printed++
	}

	if batch && printed == 0 {
		fmt.Fprintln(os.Stdout, "所有节点均无 WARN/FAIL。")
	}
	if batch {
		fmt.Fprintln(os.Stdout)
		printBoxTable(
			[]string{"TOTAL", "OK", "WARN", "FAIL", "SKIP"},
			[][]string{{
				fmt.Sprintf("%d", result.Summary.Total),
				fmt.Sprintf("%d", result.Summary.OK),
				fmt.Sprintf("%d", result.Summary.Warn),
				fmt.Sprintf("%d", result.Summary.Fail),
				fmt.Sprintf("%d", result.Summary.Skip),
			}},
		)
	}
}

func healthStatusOrder(status nodehealth.Status) int {
	switch status {
	case nodehealth.StatusFail:
		return 4
	case nodehealth.StatusWarn:
		return 3
	case nodehealth.StatusOK:
		return 2
	case nodehealth.StatusSkip:
		return 1
	default:
		return 0
	}
}
