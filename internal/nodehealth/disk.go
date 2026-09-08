package nodehealth

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"

	metricsquery "rayctl/internal/metrics"
)

const diskWarnThreshold = 85.0

type diskMountEvidence struct {
	MountPoint       string  `json:"mountpoint"`
	Device           string  `json:"device,omitempty"`
	FSType           string  `json:"fstype,omitempty"`
	UsedPercent      float64 `json:"used_percent,omitempty"`
	InodeUsedPercent float64 `json:"inode_used_percent,omitempty"`
}

func (s *Service) checkDisk(ctx context.Context, node *corev1.Node, at time.Time) (CheckResult, error) {
	if s.metrics == nil {
		return CheckResult{}, fmt.Errorf("metrics client is unavailable")
	}
	nodeValue := strconv.Quote(node.Name)
	selector := fmt.Sprintf(`nodename=%s,fstype!~"tmpfs|overlay|squashfs|dtfs|nfs.*"`, nodeValue)
	queries := []string{
		fmt.Sprintf(`100*(1-node_filesystem_avail_bytes{%s}/node_filesystem_size_bytes{%s})`, selector, selector),
		fmt.Sprintf(`100*(1-node_filesystem_files_free{%s}/node_filesystem_files{%s})`, selector, selector),
	}
	series := make([][]metricsquery.RawSeries, len(queries))
	errs := make([]error, len(queries))
	var wait sync.WaitGroup
	for index := range queries {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			series[index], errs[index] = s.metrics.Query(ctx, queries[index], at)
		}(index)
	}
	wait.Wait()
	if errs[0] != nil || errs[1] != nil {
		return CheckResult{}, fmt.Errorf("query node filesystem metrics: %w", errorsJoin(errs...))
	}

	type mountKey struct{ mount, device, fsType string }
	mounts := make(map[mountKey]*diskMountEvidence)
	apply := func(rows []metricsquery.RawSeries, inode bool) {
		for _, row := range rows {
			if len(row.Values) == 0 {
				continue
			}
			value := row.Values[len(row.Values)-1]
			if math.IsNaN(value) || math.IsInf(value, 0) {
				continue
			}
			key := mountKey{
				mount:  strings.TrimSpace(row.Metric["mountpoint"]),
				device: strings.TrimSpace(row.Metric["device"]),
				fsType: strings.TrimSpace(row.Metric["fstype"]),
			}
			item := mounts[key]
			if item == nil {
				item = &diskMountEvidence{MountPoint: key.mount, Device: key.device, FSType: key.fsType}
				mounts[key] = item
			}
			if inode {
				item.InodeUsedPercent = value
			} else {
				item.UsedPercent = value
			}
		}
	}
	apply(series[0], false)
	apply(series[1], true)
	if len(mounts) == 0 {
		return skipCheck("未查询到 node_filesystem 指标"), nil
	}

	items := make([]diskMountEvidence, 0, len(mounts))
	status := StatusOK
	maxInode := 0.0
	for _, item := range mounts {
		item.UsedPercent = roundOne(item.UsedPercent)
		item.InodeUsedPercent = roundOne(item.InodeUsedPercent)
		if item.UsedPercent > diskWarnThreshold || item.InodeUsedPercent > diskWarnThreshold {
			status = StatusWarn
		}
		if item.InodeUsedPercent > maxInode {
			maxInode = item.InodeUsedPercent
		}
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UsedPercent == items[j].UsedPercent {
			return items[i].MountPoint < items[j].MountPoint
		}
		return items[i].UsedPercent > items[j].UsedPercent
	})

	parts := make([]string, 0, minInt(4, len(items))+1)
	for _, item := range items[:minInt(4, len(items))] {
		parts = append(parts, fmt.Sprintf("%s %.1f%%", emptyMount(item.MountPoint), item.UsedPercent))
	}
	parts = append(parts, fmt.Sprintf("inode 最高 %.1f%%", maxInode))
	return CheckResult{
		Status:  status,
		Message: strings.Join(parts, "，"),
		Evidence: map[string]any{
			"threshold_percent": diskWarnThreshold,
			"mounts":            items,
		},
	}, nil
}

func roundOne(value float64) float64 {
	return math.Round(value*10) / 10
}

func emptyMount(value string) string {
	if strings.TrimSpace(value) == "" {
		return "<unknown>"
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func errorsJoin(values ...error) error {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if value != nil {
			parts = append(parts, value.Error())
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(parts, "; "))
}
