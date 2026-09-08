package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"

	"rayctl/internal/kube"
	metricsquery "rayctl/internal/metrics"
	"rayctl/internal/nodeexec"
	"rayctl/internal/nodehealth"
	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

func newNodeHealthCmd() *cobra.Command {
	var queueName string
	var vcName string
	var checksText string
	var noExec bool
	var outputFormat string

	command := &cobra.Command{
		Use:   "health <node-or-ip> [node-or-ip...]",
		Short: "检查节点基础状态、磁盘、设备插件、时钟、内核错误和 MindX 状态",
		Example: strings.Join([]string{
			"  rayctl node health host-10-140-70-107",
			"  rayctl node health 10.140.70.107 10.140.70.108",
			"  rayctl node health -q queue-d-reserved-a3-llm-share",
			"  rayctl node health -v vc-a3-ailab --no-exec",
			"  rayctl node health host-10-140-70-107 --checks node-basic,disk -o json",
		}, "\n"),
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateNodeHealthTarget(args, queueName, vcName); err != nil {
				return err
			}
			checks, err := parseNodeHealthChecks(checksText)
			if err != nil {
				return err
			}
			if err := validateNodeHealthOutput(outputFormat); err != nil {
				return err
			}

			restConfig, err := kube.NewRestConfig(kubeconfig)
			if err != nil {
				return err
			}
			clientset, err := kubernetes.NewForConfig(restConfig)
			if err != nil {
				return fmt.Errorf("create kubernetes clientset: %w", err)
			}
			platformClient, hasPlatform := platform.NewVirtualClusterClientFromEnv()
			environment := effectiveEnvironmentSelection()
			if hasPlatform {
				if config, configErr := platformClient.CurrentMetricsConfig(); configErr == nil {
					environment = config.Environment
				}
			}

			targets, batch, err := resolveNodeHealthTargets(cmd.Context(), args, queueName, vcName, clientset, platformClient)
			if err != nil {
				return err
			}

			var metricsClient *metricsquery.Client
			if nodehealth.HasCheck(checks, nodehealth.CheckDisk) {
				if !hasPlatform {
					return fmt.Errorf("disk check requires platform configuration in ~/.rayctl/platform.json")
				}
				config, configErr := platformClient.CurrentMetricsConfig()
				if configErr != nil {
					return configErr
				}
				if strings.TrimSpace(config.Username) == "" || strings.TrimSpace(config.Password) == "" {
					return fmt.Errorf("disk check requires metrics_username and metrics_password in the current platform profile")
				}
				metricsClient, err = metricsquery.NewClient(cmd.Context(), metricsquery.EndpointConfig{
					BaseURL: config.BaseURL, Username: config.Username, Password: config.Password,
					Kubeconfig: kube.ResolvedKubeconfigPath(kubeconfig), RequestTimeout: 30 * time.Second,
				})
				if err != nil {
					return err
				}
				defer metricsClient.Close()
			}

			var executor nodeexec.Runner
			if !noExec && nodeHealthChecksNeedExec(checks) {
				executor = nodeexec.NewKubernetesExecutor(restConfig, clientset)
			}
			healthService := nodehealth.NewService(clientset, metricsClient, executor, environment)
			nodes, resolveErr := healthService.ResolveNodes(cmd.Context(), targets)
			if resolveErr != nil {
				return resolveErr
			}
			result, runErr := healthService.Run(cmd.Context(), nodes, nodehealth.Options{
				Checks: checks, NoExec: noExec, MaxParallel: 4,
			})
			result.Batch = batch
			if hasPlatform {
				resolveNodeHealthVCNames(cmd.Context(), platformClient, result)
			}
			if printErr := output.PrintNodeHealth(result, outputFormat); printErr != nil {
				return printErr
			}
			if runErr != nil {
				return runErr
			}
			if result.Summary.Fail > 0 {
				requestProcessExitCode(2)
			}
			return nil
		},
	}
	command.Flags().StringVarP(&queueName, "queue", "q", "", "检查指定 SSP Queue 下的全部节点")
	command.Flags().StringVarP(&vcName, "vc", "v", "", "检查指定 VC 下的全部节点")
	command.Flags().StringVar(&checksText, "checks", "", "只运行指定检查项，逗号分隔；默认全部")
	command.Flags().BoolVar(&noExec, "no-exec", false, "跳过需要进入特权容器执行只读命令的检查项")
	command.Flags().StringVarP(&outputFormat, "output", "o", "table", "输出格式: table|json")
	return command
}

func validateNodeHealthTarget(args []string, queueName string, vcName string) error {
	queueName = strings.TrimSpace(queueName)
	vcName = strings.TrimSpace(vcName)
	selectors := 0
	if len(args) > 0 {
		selectors++
	}
	if queueName != "" {
		selectors++
	}
	if vcName != "" {
		selectors++
	}
	if selectors == 0 {
		return fmt.Errorf("specify one or more nodes, --queue, or --vc")
	}
	if selectors > 1 {
		return fmt.Errorf("node arguments, --queue, and --vc are mutually exclusive")
	}
	return nil
}

func parseNodeHealthChecks(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "all") {
		return append([]string(nil), nodehealth.DefaultChecks...), nil
	}
	valid := make(map[string]struct{}, len(nodehealth.DefaultChecks))
	for _, check := range nodehealth.DefaultChecks {
		valid[check] = struct{}{}
	}
	checks := make([]string, 0)
	seen := make(map[string]struct{})
	for _, raw := range strings.Split(value, ",") {
		check := strings.ToLower(strings.TrimSpace(raw))
		if _, ok := valid[check]; !ok {
			return nil, fmt.Errorf("unknown node health check %q; available: %s", raw, strings.Join(nodehealth.DefaultChecks, ","))
		}
		if _, duplicate := seen[check]; duplicate {
			continue
		}
		seen[check] = struct{}{}
		checks = append(checks, check)
	}
	if len(checks) == 0 {
		return nil, fmt.Errorf("--checks must contain at least one check")
	}
	return checks, nil
}

func validateNodeHealthOutput(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "table", "json":
		return nil
	default:
		return fmt.Errorf("unsupported --output %q; expected table or json", value)
	}
}

func nodeHealthChecksNeedExec(checks []string) bool {
	return nodehealth.HasCheck(checks, nodehealth.CheckDevicePlugin) ||
		nodehealth.HasCheck(checks, nodehealth.CheckClockSkew) ||
		nodehealth.HasCheck(checks, nodehealth.CheckKernelErrors)
}

func resolveNodeHealthTargets(
	ctx context.Context,
	args []string,
	queueName string,
	vcName string,
	clientset kubernetes.Interface,
	platformClient *platform.VirtualClusterClient,
) ([]string, bool, error) {
	if strings.TrimSpace(queueName) != "" {
		if platformClient == nil {
			return nil, true, fmt.Errorf("--queue requires platform configuration in ~/.rayctl/platform.json")
		}
		region, err := selectedSSPRegionForLookup()
		if err != nil {
			return nil, true, err
		}
		resourceService := service.NewSSPResourceService(clientset, platformClient)
		resourceService.SetQueueNodeClientResolver(localQueueVClusterClient)
		queueNodes, err := resourceService.ListQueueNodes(ctx, strings.TrimSpace(queueName), region)
		if err != nil {
			return nil, true, fmt.Errorf("resolve queue %q nodes: %w", queueName, err)
		}
		targets := make([]string, 0, len(queueNodes.Items))
		for _, item := range queueNodes.Items {
			if value := firstNonEmptyString(strings.TrimSpace(item.HostName), strings.TrimSpace(item.HostIP)); value != "" {
				targets = append(targets, value)
			}
		}
		if len(targets) == 0 {
			return nil, true, fmt.Errorf("queue %q has no nodes", queueName)
		}
		return targets, true, nil
	}
	if strings.TrimSpace(vcName) != "" {
		if platformClient == nil {
			return nil, true, fmt.Errorf("--vc requires platform configuration in ~/.rayctl/platform.json")
		}
		vcService := service.NewVCService(platformClient)
		vcNodes, err := vcService.ListNodes(ctx, strings.TrimSpace(vcName))
		if err != nil {
			return nil, true, fmt.Errorf("resolve vc %q nodes: %w", vcName, err)
		}
		targets := make([]string, 0, len(vcNodes.Items))
		for _, item := range vcNodes.Items {
			if value := firstNonEmptyString(strings.TrimSpace(item.HostName), strings.TrimSpace(item.HostIP)); value != "" {
				targets = append(targets, value)
			}
		}
		if len(targets) == 0 {
			return nil, true, fmt.Errorf("vc %q has no nodes", vcName)
		}
		return targets, true, nil
	}
	return append([]string(nil), args...), len(args) > 1, nil
}

func resolveNodeHealthVCNames(ctx context.Context, client *platform.VirtualClusterClient, result *nodehealth.Result) {
	if client == nil || result == nil {
		return
	}
	uids := make([]string, 0, len(result.Nodes))
	for _, node := range result.Nodes {
		if node.VCUID != "" {
			uids = append(uids, node.VCUID)
		}
	}
	if len(uids) == 0 {
		return
	}
	resolveCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	names, _, err := client.ResolveDisplayNamesWithProfiles(resolveCtx, uids)
	if err != nil {
		return
	}
	for index := range result.Nodes {
		if name := strings.TrimSpace(names[result.Nodes[index].VCUID]); name != "" {
			result.Nodes[index].Labels.VC = name
		}
	}
}
