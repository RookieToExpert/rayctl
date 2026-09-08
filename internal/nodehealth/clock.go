package nodehealth

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"rayctl/internal/nodeexec"
)

const clockSkewWarnSeconds = 300.0

func (s *Service) checkClockSkew(ctx context.Context, node *corev1.Node) (CheckResult, error) {
	if s.executor == nil {
		return CheckResult{}, fmt.Errorf("node exec is unavailable")
	}
	output, err := s.executor.Run(ctx, node.Name, nodeexec.OperationClock)
	if err != nil {
		return CheckResult{}, err
	}
	btime, uptime, now, err := parseClockOutput(output)
	if err != nil {
		return CheckResult{}, err
	}
	skew := float64(now) - (float64(btime) + uptime)
	status := StatusOK
	if math.Abs(skew) > clockSkewWarnSeconds {
		status = StatusWarn
	}
	return CheckResult{
		Status:  status,
		Message: fmt.Sprintf("now-(btime+uptime)=%.1fs", skew),
		Evidence: map[string]any{
			"skew_seconds": roundOne(skew),
			"btime":        btime,
			"uptime":       roundOne(uptime),
			"now":          now,
		},
	}, nil
}

func parseClockOutput(output string) (int64, float64, int64, error) {
	lines := nonEmptyLines(output)
	if len(lines) < 3 {
		return 0, 0, 0, fmt.Errorf("unexpected clock output: expected 3 lines, got %d", len(lines))
	}
	btimeFields := strings.Fields(lines[0])
	if len(btimeFields) != 2 || btimeFields[0] != "btime" {
		return 0, 0, 0, fmt.Errorf("unexpected btime output %q", lines[0])
	}
	btime, err := strconv.ParseInt(btimeFields[1], 10, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("parse btime: %w", err)
	}
	uptimeFields := strings.Fields(lines[1])
	if len(uptimeFields) == 0 {
		return 0, 0, 0, fmt.Errorf("uptime output is empty")
	}
	uptime, err := strconv.ParseFloat(uptimeFields[0], 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("parse uptime: %w", err)
	}
	now, err := strconv.ParseInt(strings.Fields(lines[2])[0], 10, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("parse node time: %w", err)
	}
	return btime, uptime, now, nil
}

func nonEmptyLines(value string) []string {
	result := make([]string, 0)
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			result = append(result, line)
		}
	}
	return result
}
