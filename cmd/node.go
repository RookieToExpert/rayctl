package cmd

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"rayctl/internal/kube"
	"rayctl/internal/platform"
	"rayctl/internal/service"
	"rayctl/pkg/output"
)

// newNodeCmd 创建 "node" 命令，它是所有节点相关操作的父命令。
// 该命令本身不执行具体业务，而是作为入口包含诸如 get, describe, cordon 等子命令。
func newNodeCmd() *cobra.Command {
	// 定义 node 基础命令及其描述
	nodeCmd := &cobra.Command{
		Use:   "node",
		Short: "操作 Kubernetes 节点",
	}

	// 注册子命令：获取节点列表
	nodeCmd.AddCommand(newNodeGetCmd())
	// 注册子命令：查看节点详情 (包含资源与运行中的 Pod)
	nodeCmd.AddCommand(newNodeDescribeCmd())
	// 注册子命令：封锁节点 (标记不可调度并添加维修标签)
	nodeCmd.AddCommand(newNodeCordonCmd())
	// 注册子命令：解封节点 (恢复可调度并移除维修标签)
	nodeCmd.AddCommand(newNodeUncordonCmd())

	return nodeCmd
}

// newNodeGetCmd 创建 "get" 子命令，用于根据 profile 或 label selector 获取节点列表。
func newNodeGetCmd() *cobra.Command {
	var longOutput bool
	var showAll bool

	// 定义 get 命令的结构和行为
	cmd := &cobra.Command{
		Use:   "get [profile-or-selector]",
		Short: "通过 profile 或 label selector 列出节点",
		Long: "通过 profile、label selector 或 InternalIP 片段列出节点。\n" +
			"支持直接传入 IP 过滤片段，例如 10.12.14；也支持使用 | 分隔多个片段做或匹配，例如 '10.12|10.140'。",
		Example: strings.Join([]string{
			"  rayctl node get",
			"  rayctl node get ecp",
			"  rayctl node get 'node-role.compute.sensecore.cn/prod=ecs'",
			"  rayctl node get -A 10.12.14",
			"  rayctl node get -A '10.12|10.140'",
		}, "\n"),
		// 限制最多只能接受 1 个位置参数 (作为 target)
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. 初始化 Kubernetes 客户端，使用全局的 kubeconfig 变量
			// kube.NewClientset 定义于 internal/kube/client.go
			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}
			// 2. 解析目标参数，如果有传递参数则作为 target，否则为空字符串
			target := ""
			if len(args) == 1 {
				target = args[0]
			}

			listTarget := target
			ipFilters := parseNodeIPFilters(target)
			if len(ipFilters) > 0 {
				listTarget = ""
			}

			// 3. 实例化 NodeService 并调用 List 方法获取符合条件的节点及最终使用的选择器
			// service.NewNodeService 定义于 internal/service/node_service.go
			nodeService := service.NewNodeService(clientset)
			// nodeService.List 定义于 internal/service/node_service.go
			nodes, resolvedSelector, err := nodeService.List(context.Background(), listTarget, "")
			if err != nil {
				return err
			}
			if len(ipFilters) > 0 {
				nodes = filterNodesByIP(nodes, ipFilters)
				resolvedSelector = fmt.Sprintf("InternalIP matches %s", strings.Join(ipFilters, " | "))
			}

			if vcClient, ok := platform.NewVirtualClusterClientFromEnv(); ok {
				if longOutput {
					if computeNodes, err := vcClient.ListAIComputeNodes(context.Background()); err == nil {
						byHostName := make(map[string]platform.AIComputeNode, len(computeNodes))
						byHostIP := make(map[string]platform.AIComputeNode, len(computeNodes))
						for _, item := range computeNodes {
							if key := strings.TrimSpace(item.Properties.HostName); key != "" {
								byHostName[key] = item
							}
							if key := strings.TrimSpace(item.Properties.HostIP); key != "" {
								byHostIP[key] = item
							}
						}
						for i := range nodes {
							item, ok := byHostName[strings.TrimSpace(nodes[i].Name)]
							if !ok {
								item, ok = byHostIP[strings.TrimSpace(nodes[i].InternalIP)]
							}
							if !ok {
								continue
							}
							if strings.TrimSpace(nodes[i].Tenant) == "" {
								nodes[i].Tenant = strings.TrimSpace(item.ProfileName)
							}
							if strings.TrimSpace(nodes[i].ClusterName) == "" || strings.HasPrefix(strings.TrimSpace(nodes[i].ClusterName), "vc-") {
								if vcName := strings.TrimSpace(item.Properties.VirtualClusterName); vcName != "" {
									nodes[i].ClusterName = vcName
								}
							}
						}
					}
				}

				clusterUIDs := make([]string, 0, len(nodes))
				for _, node := range nodes {
					if strings.Contains(strings.ToLower(strings.TrimSpace(node.ProdRole)), "ecs") {
						continue
					}
					if node.ClusterUID != "" && (strings.TrimSpace(node.ClusterName) == "" || strings.HasPrefix(strings.TrimSpace(node.ClusterName), "vc-") || strings.TrimSpace(node.Tenant) == "") {
						clusterUIDs = append(clusterUIDs, node.ClusterUID)
					}
				}

				displayNames, profileNames, err := vcClient.ResolveDisplayNamesWithProfiles(context.Background(), clusterUIDs)
				if err == nil {
					for i := range nodes {
						if displayName, ok := displayNames[nodes[i].ClusterUID]; ok {
							nodes[i].ClusterName = displayName
						}
						if tenant, ok := profileNames[nodes[i].ClusterUID]; ok {
							nodes[i].Tenant = tenant
						}
					}
				}
			}

			for i := range nodes {
				if strings.Contains(strings.ToLower(strings.TrimSpace(nodes[i].ProdRole)), "ecs") {
					nodes[i].ClusterName = "ecs 节点"
					if longOutput {
						nodes[i].Tenant = "ecs 节点"
					}
					continue
				}
				if longOutput {
					if strings.TrimSpace(nodes[i].Tenant) != "" {
						continue
					}
					nodes[i].Tenant = "控制面节点"
					if strings.TrimSpace(nodes[i].ClusterName) == "" {
						nodes[i].ClusterName = "控制面节点"
					}
				}
			}

			displayLimit := 100
			if showAll {
				displayLimit = 0
			}

			displayNodes, start, end := limitNodes(nodes, displayLimit)

			// 4. 调用 output 包将节点列表以表格等形式打印到终端
			// output.PrintNodeList 定义于 pkg/output/table.go
			showProdRole := true
			switch strings.ToLower(strings.TrimSpace(target)) {
			case "ecp", "ecs":
				showProdRole = false
			}
			output.PrintNodeList(displayNodes, resolvedSelector, len(nodes), start, end, displayLimit, showProdRole, longOutput)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&showAll, "all", "A", false, "Show all nodes")
	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "Show additional detail columns such as tenant")

	return cmd
}

func limitNodes(nodes []service.NodeListItem, limit int) ([]service.NodeListItem, int, int) {
	total := len(nodes)
	if limit <= 0 || total == 0 {
		if total == 0 {
			return nodes, 0, 0
		}
		return nodes, 1, total
	}
	if limit > total {
		limit = total
	}
	return nodes[:limit], 1, limit
}

// newNodeDescribeCmd 创建 "describe" 子命令，用于展示特定节点的详细状态。
// 包括该节点的可分配资源量 (Allocatable) 以及当前正在该节点上运行的 Pod 列表。
func newNodeDescribeCmd() *cobra.Command {
	var debugTiming bool

	// 定义 describe 命令的结构和行为
	cmd := &cobra.Command{
		Use:     "describe <node-name> [node-name...]",
		Aliases: []string{"check"},
		Short:   "快速检查节点上的 vcluster Pod 和资源占用，支持节点名或 IP",
		Long:    "快速检查节点上的 vcluster Pod 和资源占用，支持直接传入节点名，或传入形如 10.140.214.222 的节点 IP。",
		Example: strings.Join([]string{
			"  rayctl node check host-10-140-214-222",
			"  rayctl node check 10.140.214.222",
		}, "\n"),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientBegin := time.Now()
			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}
			clientDuration := time.Since(clientBegin)

			nodeService := service.NewNodeService(clientset)
			vcClient, hasVCClient := platform.NewVirtualClusterClientFromEnv()
			resolvedVCNames := make(map[string]string)

			for i, nodeName := range args {
				resolvedNodeName := normalizeNodeIdentifier(nodeName)
				details, err := nodeService.Describe(cmd.Context(), resolvedNodeName)
				if err != nil {
					return fmt.Errorf("node %q: %w", nodeName, err)
				}

				resolveVCBegin := time.Now()
				vcUID := strings.TrimSpace(details.VClusterUID)
				if hasVCClient && vcUID != "" {
					if cachedName, ok := resolvedVCNames[vcUID]; ok {
						details.VClusterName = cachedName
					} else {
						resolveCtx, cancel := context.WithTimeout(cmd.Context(), 2*time.Second)
						resource, resolveErr := vcClient.FindResourceByUID(resolveCtx, vcUID, "virtualClusters")
						cancel()
						if resolveErr == nil && strings.TrimSpace(resource.Name) != "" {
							details.VClusterName = strings.TrimSpace(resource.Name)
						}
						resolvedVCNames[vcUID] = details.VClusterName
					}
				}
				details.Timings.ResolveVC = time.Since(resolveVCBegin)
				if i > 0 {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				output.PrintNodeDescribe(details, debugTiming, clientDuration)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&debugTiming, "debug-timing", false, "Print timing diagnostics for node check")

	return cmd
}

func parseNodeIPFilters(target string) []string {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil
	}
	if _, ok := map[string]struct{}{"ecp": {}, "ecs": {}}[strings.ToLower(target)]; ok {
		return nil
	}
	if strings.ContainsAny(target, "=,") || strings.HasPrefix(target, "host-") {
		return nil
	}
	parts := strings.Split(target, "|")
	filters := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if looksLikeIPFragment(part) {
			filters = append(filters, part)
		} else {
			return nil
		}
	}
	return filters
}

func looksLikeIPFragment(value string) bool {
	for _, r := range value {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return strings.Contains(value, ".")
}

func filterNodesByIP(nodes []service.NodeListItem, filters []string) []service.NodeListItem {
	if len(filters) == 0 {
		return nodes
	}
	filtered := make([]service.NodeListItem, 0, len(nodes))
	for _, node := range nodes {
		ip := strings.TrimSpace(node.InternalIP)
		for _, filter := range filters {
			if strings.Contains(ip, filter) {
				filtered = append(filtered, node)
				break
			}
		}
	}
	return filtered
}

func normalizeNodeIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "host-") {
		return value
	}
	if ip := net.ParseIP(value); ip != nil && strings.Contains(value, ".") {
		return "host-" + strings.ReplaceAll(value, ".", "-")
	}
	return value
}

// newNodeCordonCmd 创建 "cordon" 子命令，用于将指定节点置于不可调度 (Cordon) 状态。
// 该操作会阻止新的 Pod 被调度到该节点，同时会给节点打上特定的维修标记。
func newNodeCordonCmd() *cobra.Command {
	// 定义 cordon 命令的结构和行为
	cmd := &cobra.Command{
		Use:   "cordon <node-name>",
		Short: "封锁节点并将其标记为维修状态",
		// 强制要求必须提供 1 个位置参数，即节点名称
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. 初始化 Kubernetes 客户端
			// kube.NewClientset 定义于 internal/kube/client.go
			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}

			// 2. 实例化 NodeService，对指定节点执行 Cordon 和打维修标签的组合逻辑
			// service.NewNodeService 定义于 internal/service/node_service.go
			nodeService := service.NewNodeService(clientset)
			// nodeService.Cordon 定义于 internal/service/node_service.go
			result, err := nodeService.Cordon(context.Background(), args[0])
			if err != nil {
				return err
			}

			// 3. 打印节点状态变更的结果
			// output.PrintNodeMutationResult 定义于 pkg/output/table.go
			output.PrintNodeMutationResult(result)
			return nil
		},
	}

	return cmd
}

// newNodeUncordonCmd 创建 "uncordon" 子命令，用于将指定节点恢复为正常可调度状态。
// 该操作会允许新的 Pod 重新被调度到该节点，同时会清除之前打上的维修标记。
func newNodeUncordonCmd() *cobra.Command {
	// 定义 uncordon 命令的结构和行为
	cmd := &cobra.Command{
		Use:   "uncordon <node-name>",
		Short: "解封节点并清除维修标签",
		// 强制要求必须提供 1 个位置参数，即节点名称
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. 初始化 Kubernetes 客户端
			// kube.NewClientset 定义于 internal/kube/client.go
			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}

			// 2. 实例化 NodeService，对指定节点执行 Uncordon 和移除维修标签的组合逻辑
			// service.NewNodeService 定义于 internal/service/node_service.go
			nodeService := service.NewNodeService(clientset)
			// nodeService.Uncordon 定义于 internal/service/node_service.go
			result, err := nodeService.Uncordon(context.Background(), args[0])
			if err != nil {
				return err
			}

			// 3. 打印节点状态恢复的结果
			// output.PrintNodeMutationResult 定义于 pkg/output/table.go
			output.PrintNodeMutationResult(result)
			return nil
		},
	}

	return cmd
}
