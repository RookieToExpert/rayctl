package cmd

import (
	"context"
	"fmt"

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
	return command
}

func explicitHCKubeconfig(cmd *cobra.Command) bool {
	return cmd.Flags().Changed("kubeconfig") || cmd.InheritedFlags().Changed("kubeconfig")
}

func hcEnvironmentForVC(detail *service.VCDetailResult, explicit bool) (service.HCEnvironment, error) {
	var zero service.HCEnvironment
	if effectiveEnvironmentSelection() == "dcloud" {
		return zero, fmt.Errorf("Dcloud 控制面规格接口尚未验证；请使用 --platform-only")
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
	dynamicClient, err := kube.NewDynamicClient(target.Kubeconfig)
	if err != nil {
		return nil, err
	}
	return service.NewVCConfigService(target.Client, dynamicClient, platform.NewVCConfigClient(target.Client.CoreV1().RESTClient())), nil
}

func enrichVCControlPlane(ctx context.Context, detail *service.VCDetailResult, target service.HCEnvironment) {
	configService, err := vcConfigServiceFor(target)
	if err != nil {
		detail.ControlPlane = &service.VCControlPlaneConfig{Warnings: []string{err.Error()}}
		return
	}
	// VC details remain useful when the operation API is unavailable. The
	// snapshot retains the HC label and precise warnings instead of guessing.
	detail.ControlPlane, _ = configService.Get(ctx, detail.Name, detail.UID, target.Name)
}
