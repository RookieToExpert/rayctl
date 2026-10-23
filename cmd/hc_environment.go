package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"rayctl/internal/kube"
	"rayctl/internal/service"
)

func hcTargets(cmd *cobra.Command) ([]service.HCEnvironment, error) {
	explicit := cmd.Flags().Changed("kubeconfig") || cmd.InheritedFlags().Changed("kubeconfig")
	return resolveHCTargets(effectiveEnvironmentSelection(), kube.ResolvedKubeconfigPath(kubeconfig), explicit)
}

func resolveHCTargets(selection, current string, explicit bool) ([]service.HCEnvironment, error) {
	if selection == "" || selection == "auto" {
		selection = inferEnvironmentFromKubeconfigPath(current)
	}
	if selection == "" {
		selection = "d"
	}
	names := []string{selection}
	if selection == "all" {
		names = []string{"d", "pt"}
	}
	currentEnv := inferEnvironmentFromKubeconfigPath(current)
	for _, name := range []string{"d", "pt", "dcloud"} {
		if configured := strings.TrimSpace(os.Getenv("RAYCTL_HC_KUBECONFIG_" + strings.ToUpper(name))); configured != "" && filepath.Clean(configured) == filepath.Clean(current) {
			currentEnv = name
		}
	}
	if explicit && selection != "all" && currentEnv != "" && selection != currentEnv {
		return nil, fmt.Errorf("-k 属于 %s，与 -e %s 冲突；请指定相应 HC kubeconfig", currentEnv, selection)
	}
	if explicit && selection == "all" && currentEnv == "" {
		return nil, fmt.Errorf("无法识别 -k 的 HC 环境；-e all 请使用可识别路径或 RAYCTL_HC_KUBECONFIG_D/PT")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	var targets []service.HCEnvironment
	for _, name := range names {
		region, filename := "cn-pj-01", "kubeconfig"
		switch name {
		case "pt":
			region, filename = "cn-pj-03", "kubeconfigpt"
		case "dcloud":
			filename = "kubeconfigc"
		case "d":
		default:
			return nil, fmt.Errorf("unsupported HC environment %q", name)
		}
		path := strings.TrimSpace(os.Getenv("RAYCTL_HC_KUBECONFIG_" + strings.ToUpper(name)))
		if path == "" {
			path = filepath.Join(home, filename)
			if currentEnv == name || (explicit && selection != "all") || (currentEnv == "" && len(names) == 1) {
				path = current
			}
		}
		target := service.HCEnvironment{Name: name, Region: region, Kubeconfig: path}
		target.Client, target.Err = kube.NewClientset(path)
		targets = append(targets, target)
	}
	return targets, nil
}
