package nodehealth

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"rayctl/internal/nodeexec"
)

const (
	kernelLookback      = 24 * time.Hour
	kernelLinesPerClass = 20
)

var (
	dmesgLinePattern = regexp.MustCompile(`^\[\s*([0-9]+(?:\.[0-9]+)?)\]\s*(.*)$`)
	kernelPatterns   = []kernelCategoryPattern{
		{Name: "oom", Pattern: regexp.MustCompile(`(?i)(Out of memory:|Memory cgroup out of memory|oom-kill:)`)},
		{Name: "io", Pattern: regexp.MustCompile(`(?i)(I/O error|Buffer I/O error|critical medium error)`)},
		{Name: "panic", Pattern: regexp.MustCompile(`(?i)(Call Trace:|kernel BUG at|general protection fault|\bOops\b)`)},
		{Name: "fs", Pattern: regexp.MustCompile(`(?i)(EXT4-fs error|XFS.*Internal error)`)},
		{Name: "rdma", Pattern: regexp.MustCompile(`(?i)(mlx5_core.*error|hfi1.*error)`)},
	}
	beijingLocation = time.FixedZone("UTC+8", 8*60*60)
)

type kernelCategoryPattern struct {
	Name    string
	Pattern *regexp.Regexp
}

type kernelLine struct {
	Monotonic float64
	Text      string
}

type kernelCategoryEvidence struct {
	Category string   `json:"category"`
	Count    int      `json:"count"`
	Lines    []string `json:"lines"`
	Omitted  int      `json:"omitted"`
}

func (s *Service) checkKernelErrors(ctx context.Context, node *corev1.Node) (CheckResult, error) {
	if s.executor == nil {
		return CheckResult{}, fmt.Errorf("node exec is unavailable")
	}
	output, err := s.executor.Run(ctx, node.Name, nodeexec.OperationKernelLog)
	if err != nil {
		return CheckResult{}, err
	}
	btime, now, lines, err := parseKernelOutput(output)
	if err != nil {
		return CheckResult{}, err
	}
	evidence := classifyKernelLines(lines, btime, now)
	if len(evidence) == 0 {
		return CheckResult{
			Status:   StatusOK,
			Message:  "近 24h 未匹配到 OOM/IO/panic/fs/RDMA 内核错误",
			Evidence: map[string]any{"window_hours": 24, "categories": []kernelCategoryEvidence{}},
		}, nil
	}

	status := StatusWarn
	parts := make([]string, 0)
	for _, category := range evidence {
		if category.Category == "panic" && category.Count > 0 {
			status = StatusFail
		}
		parts = append(parts, fmt.Sprintf("%s=%d", category.Category, category.Count))
	}
	return CheckResult{
		Status:  status,
		Message: strings.Join(parts, "; ") + "; 原文见 -o json 的 evidence（每类最多 20 行）",
		Evidence: map[string]any{
			"window_hours": 24,
			"categories":   evidence,
		},
	}, nil
}

func parseKernelOutput(output string) (int64, int64, []kernelLine, error) {
	rawLines := strings.Split(output, "\n")
	first := -1
	second := -1
	for index, line := range rawLines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if first < 0 {
			first = index
			continue
		}
		second = index
		break
	}
	if first < 0 || second < 0 {
		return 0, 0, nil, fmt.Errorf("unexpected kernel log output")
	}
	btimeFields := strings.Fields(strings.TrimSpace(rawLines[first]))
	if len(btimeFields) != 2 || btimeFields[0] != "btime" {
		return 0, 0, nil, fmt.Errorf("unexpected btime output %q", rawLines[first])
	}
	btime, err := strconv.ParseInt(btimeFields[1], 10, 64)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("parse btime: %w", err)
	}
	now, err := strconv.ParseInt(strings.TrimSpace(rawLines[second]), 10, 64)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("parse node time: %w", err)
	}
	lines := make([]kernelLine, 0, len(rawLines)-second-1)
	for _, line := range rawLines[second+1:] {
		matches := dmesgLinePattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) != 3 {
			continue
		}
		monotonic, parseErr := strconv.ParseFloat(matches[1], 64)
		if parseErr != nil {
			continue
		}
		lines = append(lines, kernelLine{Monotonic: monotonic, Text: strings.TrimSpace(matches[2])})
	}
	return btime, now, lines, nil
}

func classifyKernelLines(lines []kernelLine, btime int64, now int64) []kernelCategoryEvidence {
	cutoff := float64(now - int64(kernelLookback.Seconds()))
	result := make([]kernelCategoryEvidence, 0)
	for _, category := range kernelPatterns {
		matched := make([]string, 0)
		count := 0
		for index, line := range lines {
			absolute := float64(btime) + line.Monotonic
			if absolute < cutoff || absolute > float64(now)+60 || !category.Pattern.MatchString(line.Text) {
				continue
			}
			count++
			end := index + 1
			if category.Name == "panic" {
				end = minInt(index+4, len(lines))
			}
			for _, included := range lines[index:end] {
				if len(matched) >= kernelLinesPerClass {
					break
				}
				when := time.Unix(btime+int64(included.Monotonic), 0).In(beijingLocation).Format("2006-01-02 15:04:05")
				matched = append(matched, fmt.Sprintf("%s %s", when, included.Text))
			}
		}
		if count == 0 {
			continue
		}
		omitted := 0
		if category.Name == "panic" {
			omitted = maxInt(0, count*4-len(matched))
		} else {
			omitted = maxInt(0, count-len(matched))
		}
		result = append(result, kernelCategoryEvidence{Category: category.Name, Count: count, Lines: matched, Omitted: omitted})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Category == "panic" {
			return true
		}
		if result[j].Category == "panic" {
			return false
		}
		return result[i].Category < result[j].Category
	})
	return result
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
