package cmd

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"rayctl/internal/kube"
	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newVCConfigCmd() *cobra.Command {
	command := &cobra.Command{Use: "config", Short: "查询或更新 VC 控制面规格"}
	command.AddCommand(&cobra.Command{
		Use: "get <vc-name-or-uid>", Short: "查看平台期望规格、HC 已下发规格和实际控制面资源", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, vcService, err := newVCPlatformService()
			if err != nil {
				return err
			}
			detail, err := vcService.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			target, err := hcEnvironmentForVC(detail, explicitHCKubeconfig(cmd))
			if err != nil {
				return err
			}
			configService, err := vcConfigServiceFor(target)
			if err != nil {
				return err
			}
			result, err := configService.Get(cmd.Context(), detail.Name, detail.UID, target.Name)
			output.PrintVCConfig(result)
			return err
		},
	})
	command.AddCommand(newVCConfigUpdateCmd())
	return command
}

func newVCConfigUpdateCmd() *cobra.Command {
	var flavor string
	var dryRun, yes, allowUnlimit bool
	var wait bool
	var timeout time.Duration
	command := &cobra.Command{
		Use: "update <vc-name-or-uid>", Short: "持久更新 VC 控制面规格并等待资源和副本收敛",
		Long: "从平台读取规格定义后更新控制面规格。默认先展示差异并确认；--dry-run 仅预检。\n" +
			"medium/large 均为 3 副本，但资源不同。unlimit 不建议使用，须额外显式允许。",
		Example: "  rayctl vc config update vc-demo --flavor medium --dry-run -e d\n" +
			"  rayctl vc config update vc-demo --flavor large --yes --timeout 5m -e pt",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flavor = strings.ToLower(strings.TrimSpace(flavor))
			if err := service.ValidateVCFlavor(flavor, allowUnlimit); err != nil {
				return err
			}
			if timeout <= 0 {
				return fmt.Errorf("--timeout 必须大于 0")
			}
			if output.IsJSON() && !dryRun && !yes {
				return fmt.Errorf("JSON 更新须指定 --yes，或使用 --dry-run 预检")
			}
			_, vcService, err := newVCPlatformService()
			if err != nil {
				return err
			}
			detail, err := vcService.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			target, err := hcEnvironmentForVC(detail, explicitHCKubeconfig(cmd))
			if err != nil {
				return err
			}
			configService, err := vcConfigServiceFor(target)
			if err != nil {
				return err
			}
			result, err := configService.PrepareUpdate(cmd.Context(), detail.Name, detail.UID, target.Name, flavor, allowUnlimit)
			if err != nil {
				return err
			}
			if dryRun {
				result.DryRun = true
				result.Status = "dry-run，未提交变更"
				output.PrintVCConfigUpdate(result)
				return nil
			}
			if !yes && result.Changed {
				output.PrintVCConfigUpdate(result)
				fmt.Fprintf(cmd.OutOrStdout(), "将更新 %s/%s 的控制面规格 %s → %s，是否继续? (y/N): ", target.Name, detail.Name, result.Current.Desired.Name, result.Target.Name)
				line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if err != nil {
					return err
				}
				if !isYes(line) {
					fmt.Fprintln(cmd.OutOrStdout(), "已取消更新。")
					return nil
				}
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			err = configService.ApplyUpdate(ctx, result, wait)
			output.PrintVCConfigUpdate(result)
			return err
		},
	}
	command.Flags().StringVar(&flavor, "flavor", "", "目标规格：minimum、small、medium、large")
	_ = command.MarkFlagRequired("flavor")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "只读预检，展示当前与目标规格，不提交变更")
	command.Flags().BoolVarP(&yes, "yes", "y", false, "跳过交互确认")
	command.Flags().BoolVar(&allowUnlimit, "allow-unlimit", false, "允许使用无资源上限的 unlimit 规格")
	command.Flags().BoolVar(&wait, "wait", true, "等待 HC 标签、控制面资源和 Deployment 完成收敛")
	command.Flags().DurationVar(&timeout, "timeout", 2*time.Minute, "提交及等待收敛的最长时间")
	return command
}

func explicitHCKubeconfig(cmd *cobra.Command) bool {
	return cmd.Flags().Changed("kubeconfig") || cmd.InheritedFlags().Changed("kubeconfig")
}

func hcEnvironmentForVC(detail *service.VCDetailResult, explicit bool) (service.HCEnvironment, error) {
	var zero service.HCEnvironment
	if effectiveEnvironmentSelection() == "dcloud" {
		targets, err := resolveHCTargets("dcloud", kube.ResolvedKubeconfigPath(kubeconfig), explicit)
		if err != nil {
			return zero, err
		}
		return targets[0], targets[0].Err
	}
	environment := ""
	switch detail.Region {
	case "cn-pj-01":
		environment = "d"
	case "cn-pj-03":
		environment = "pt"
	default:
		return zero, fmt.Errorf("尚不支持 region %q 的 HC 控制面查询", detail.Region)
	}
	selection := environment
	if effectiveEnvironmentSelection() == "all" {
		selection = "all"
	}
	targets, err := resolveHCTargets(selection, kube.ResolvedKubeconfigPath(kubeconfig), explicit)
	if err != nil {
		return zero, err
	}
	for _, target := range targets {
		if target.Name == environment {
			return target, target.Err
		}
	}
	return zero, fmt.Errorf("未找到 %s 的 HC 配置", environment)
}

func vcConfigServiceFor(target service.HCEnvironment) (*service.VCConfigService, error) {
	if target.Name == "dcloud" {
		return nil, fmt.Errorf("Dcloud 控制面规格接口尚未验证，未采集规格")
	}
	dynamicClient, err := kube.NewDynamicClient(target.Kubeconfig)
	if err != nil {
		return nil, err
	}
	return service.NewVCConfigService(target.Client, dynamicClient, platform.NewVCConfigClient(target.Client.CoreV1().RESTClient())), nil
}

func enrichVCControlPlane(ctx context.Context, detail *service.VCDetailResult, target service.HCEnvironment) {
	configService, err := vcConfigServiceFor(target)
	if err != nil {
		detail.ControlPlane = &service.VCControlPlaneConfig{Name: detail.Name, UID: detail.UID, Environment: target.Name, Warnings: []string{err.Error()}}
		return
	}
	// VC details remain useful when the operation API is unavailable. The
	// snapshot retains the HC label and precise warnings instead of guessing.
	detail.ControlPlane, _ = configService.Get(ctx, detail.Name, detail.UID, target.Name)
}
