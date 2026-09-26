package output

import (
	"fmt"
	"os"
	"rayctl/internal/service"
	"strconv"
	"strings"
)

func PrintPolicyUpdateResult(result *service.PolicyUpdateResult) {
	if result == nil {
		return
	}

	status := "updated"
	if result.AlreadyPresent {
		status = "already exists"
	}

	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"POLICY", emptyDash(result.PolicyName)},
			{"VC", emptyDash(result.ClusterName)},
			{"VC UID", emptyDash(result.ClusterUID)},
			{"RULE", emptyDash(result.RuleName)},
			{"SELECTOR", emptyDash(result.SelectorKey + "=" + result.SelectorValue)},
			{"RESULT", status},
		},
		[]int{14, 96},
		tableOptions{
			noWrapCells: makeNoWrapCells(
				[2]int{0, 1},
				[2]int{1, 1},
				[2]int{2, 1},
				[2]int{3, 1},
				[2]int{4, 1},
				[2]int{5, 1},
			),
			minWidths: []int{10, 28},
		},
	)
}

func PrintSupportedClusterPolicies(items []service.SupportedPolicyItem) {
	if captureJSON(items) {
		return
	}
	rows := make([][]string, 0, maxInt(1, len(items)))
	if len(items) == 0 {
		rows = append(rows, []string{"-", "-"})
	} else {
		for _, item := range items {
			rows = append(rows, []string{
				emptyDash(item.PolicyName),
				emptyDash(strings.Join(item.RuleNames, ", ")),
			})
		}
	}

	printBoxTableWithOptions(
		[]string{"POLICY", "RULES"},
		rows,
		[]int{40, 64},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCellsForSingleColumn(len(rows), 0)...),
			minWidths:   []int{24, 20},
		},
	)
}

func PrintPolicyGetResult(result *service.PolicyGetResult) {
	if captureJSON(result) {
		return
	}
	if result == nil {
		return
	}

	if strings.TrimSpace(result.TargetCluster) != "" || strings.TrimSpace(result.TargetUID) != "" {
		matchStatus := "no"
		if result.Matched {
			matchStatus = "yes"
		}
		selector := strings.TrimSpace(result.TargetSelector)
		if len(result.Items) > 0 {
			selectors := make([]string, 0, len(result.Items))
			for _, item := range result.Items {
				selectors = append(selectors, policySelectorText(item))
			}
			selector = strings.Join(selectors, "; ")
		}

		printBoxTableWithOptions(
			[]string{"FIELD", "VALUE"},
			[][]string{
				{"POLICY", emptyDash(result.PolicyName)},
				{"VC", emptyDash(result.TargetCluster)},
				{"VC UID", emptyDash(result.TargetUID)},
				{"MATCH", matchStatus},
				{"SELECTOR", emptyDash(selector)},
			},
			[]int{14, 112},
			tableOptions{
				noWrapCells: makeNoWrapCells(
					[2]int{0, 1},
					[2]int{1, 1},
					[2]int{2, 1},
					[2]int{3, 1},
					[2]int{4, 1},
				),
				minWidths: []int{10, 28},
			},
		)
		return
	}

	printBoxTableWithOptions(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"POLICY", emptyDash(result.PolicyName)},
			{"白名单 VC 数量", strconv.Itoa(len(result.Items))},
		},
		[]int{18, 64},
		tableOptions{
			noWrapCells: makeNoWrapCells([2]int{0, 1}, [2]int{1, 1}),
			minWidths:   []int{14, 24},
		},
	)

	fmt.Fprintln(os.Stdout)
	rows := make([][]string, 0, maxInt(1, len(result.Items)))
	if len(result.Items) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-"})
	} else {
		for _, item := range result.Items {
			rows = append(rows, []string{
				emptyDash(item.ClusterName),
				emptyDash(item.ClusterUID),
				emptyDash(item.Tenant),
				emptyDash(policySelectorText(item)),
			})
		}
	}

	noWrapCells := make([][2]int, 0, len(rows)*4)
	noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), 0)...)
	noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), 1)...)
	noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), 2)...)
	noWrapCells = append(noWrapCells, noWrapCellsForSingleColumn(len(rows), 3)...)
	printBoxTableWithOptions(
		[]string{"VC", "UID", "TENANT", "SELECTOR"},
		rows,
		[]int{32, 36, 18, 96},
		tableOptions{
			noWrapCells: makeNoWrapCells(noWrapCells...),
			minWidths:   []int{20, 32, 12, 28},
		},
	)
}

func PrintPolicyGetSummary(results []*service.PolicyGetResult) {
	if captureJSON(results) {
		return
	}
	rows := make([][]string, 0, len(results))
	for _, result := range results {
		match := "no"
		if result.Matched {
			match = "yes"
		}
		rows = append(rows, []string{emptyDash(result.PolicyName), emptyDash(result.TargetCluster), emptyDash(result.TargetUID), match})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-"})
	}
	printBoxTableWithOptions([]string{"POLICY", "VC", "VC UID", "WHITELISTED"}, rows, []int{36, 32, 36, 12}, tableOptions{minWidths: []int{20, 14, 36, 11}})
}

func policySelectorText(item service.PolicyWhitelistItem) string {
	selectorKey := strings.TrimSpace(item.SelectorKey)
	selectorValue := strings.TrimSpace(item.SelectorValue)
	if selectorKey == "" {
		return selectorValue
	}
	if selectorValue == "" {
		return selectorKey
	}
	return selectorKey + "=" + selectorValue
}
