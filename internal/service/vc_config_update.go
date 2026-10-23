package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"rayctl/internal/platform"
)

type VCConfigUpdater interface {
	UpdateFlavor(context.Context, string, string) (*platform.VCOperationResource, error)
}

type VCConfigUpdateResult struct {
	Current  *VCControlPlaneConfig         `json:"current"`
	Target   platform.VCControlPlaneFlavor `json:"target"`
	Observed *VCControlPlaneConfig         `json:"observed,omitempty"`
	Changed  bool                          `json:"changed"`
	DryRun   bool                          `json:"dry_run"`
	Status   string                        `json:"status"`
}

func ValidateVCFlavor(name string, allowUnlimit bool) error {
	switch name {
	case "minimum", "small", "medium", "large":
		return nil
	case "unlimit":
		if allowUnlimit {
			return nil
		}
		return fmt.Errorf("unlimit 没有资源上限且不建议使用；确需使用时显式添加 --allow-unlimit")
	default:
		return fmt.Errorf("无效控制面规格 %q；可选 minimum、small、medium、large", name)
	}
}

func (s *VCConfigService) PrepareUpdate(ctx context.Context, name, uid, environment, flavor string, allowUnlimit bool) (*VCConfigUpdateResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := ValidateVCFlavor(flavor, allowUnlimit); err != nil {
		return nil, err
	}
	current, err := s.Get(ctx, name, uid, environment)
	if err != nil {
		return nil, fmt.Errorf("规格预检未完整完成: %w", err)
	}
	if current.Desired == nil {
		return nil, fmt.Errorf("无法确认平台当前期望规格，不能更新")
	}
	flavors, err := s.api.Flavors(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询可用控制面规格: %w", err)
	}
	var names []string
	for _, target := range flavors {
		names = append(names, target.Name)
		if target.Name == flavor {
			if target.Replicas <= 0 || len(target.ResourceRequests) == 0 || target.MaxNodeCount <= 0 {
				return nil, fmt.Errorf("平台返回的 %s 规格定义不完整", flavor)
			}
			return &VCConfigUpdateResult{Current: current, Target: target, Changed: current.Desired.Name != flavor, Status: "预检完成"}, nil
		}
	}
	return nil, fmt.Errorf("平台未提供 %s 规格；当前可用 %s", flavor, strings.Join(names, ", "))
}

func (s *VCConfigService) ApplyUpdate(ctx context.Context, result *VCConfigUpdateResult, wait bool) error {
	if result == nil || result.Current == nil || result.Current.Desired == nil || result.Target.Name == "" {
		return fmt.Errorf("控制面更新缺少完整预检结果")
	}
	if result.DryRun {
		result.Status = "dry-run，未提交变更"
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// A second read detects changes made during the confirmation prompt. The API
	// has no conditional-write token; this is a preflight check, not atomic CAS.
	current, err := s.api.Get(ctx, result.Current.UID)
	if err != nil {
		return fmt.Errorf("提交前重新查询规格: %w", err)
	}
	if current == nil || current.ControlPlaneFlavor == nil || current.ControlPlaneFlavor.Name != result.Current.Desired.Name {
		return fmt.Errorf("预检后平台期望规格已变化，请重新执行 config get 并生成预检")
	}
	if result.Changed {
		flavors, err := s.api.Flavors(ctx)
		if err != nil {
			return fmt.Errorf("提交前校验规格定义: %w", err)
		}
		definitionUnchanged := false
		for _, flavor := range flavors {
			if flavor.Name == result.Target.Name && flavor.Replicas == result.Target.Replicas && flavor.MaxNodeCount == result.Target.MaxNodeCount && sameVCResources(flavor.ResourceRequests, result.Target.ResourceRequests) && sameVCResources(flavor.ResourceLimits, result.Target.ResourceLimits) {
				definitionUnchanged = true
				break
			}
		}
		if !definitionUnchanged {
			return fmt.Errorf("预检后目标规格定义已变化，请重新预检")
		}
		updater, ok := s.api.(VCConfigUpdater)
		if !ok {
			return fmt.Errorf("平台客户端不支持持久规格更新")
		}
		if _, err := updater.UpdateFlavor(ctx, result.Current.UID, result.Target.Name); err != nil {
			result.Status = "提交结果未确认，请用 vc config get 核对"
			return fmt.Errorf("%s: %w", result.Status, err)
		}
		result.Status = "更新请求已接收，等待控制面收敛"
	} else {
		result.Status = "期望规格相同，未提交变更"
	}
	if !wait {
		return nil
	}
	return s.waitForConfig(ctx, result, time.Second)
}

func (s *VCConfigService) waitForConfig(ctx context.Context, result *VCConfigUpdateResult, interval time.Duration) error {
	var lastErr error
	for {
		observed, err := s.Get(ctx, result.Current.Name, result.Current.UID, result.Current.Environment)
		result.Observed = observed
		lastErr = err
		if err == nil && observed.ConvergedFor(&result.Target) {
			result.Status = "控制面规格已收敛"
			return nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			result.Status = "等待结束，尚未确认收敛；请用 vc config get 核对"
			if lastErr != nil {
				return fmt.Errorf("%s: %w (最后查询: %v)", result.Status, ctx.Err(), lastErr)
			}
			return fmt.Errorf("%s: %w", result.Status, ctx.Err())
		case <-timer.C:
		}
	}
}
