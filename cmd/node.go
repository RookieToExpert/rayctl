package cmd

import (
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
	// labels 变量用于接收命令行传递的 --labels 参数值
	var labels string
	var limit int
	var showAll bool
	var prod string
	var role string
	var repairOnly bool

	// 定义 get 命令的结构和行为
	cmd := &cobra.Command{
		Use:   "get [profile-or-selector]",
		Short: "通过 profile 或 label selector 列出节点",
		// 限制最多只能接受 1 个位置参数 (作为 target)
		Args:  cobra.MaximumNArgs(1),
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

			effectiveLabels := labels
			if prod != "" {
				effectiveLabels = appendLabelSelector(effectiveLabels, "node-role.compute.sensecore.cn/prod="+prod)
			}
			// 3. 实例化 NodeService 并调用 List 方法获取符合条件的节点及最终使用的选择器
			// service.NewNodeService 定义于 internal/service/node_service.go
			nodeService := service.NewNodeService(clientset)
			// nodeService.List 定义于 internal/service/node_service.go
			nodes, resolvedSelector, err := nodeService.List(context.Background(), target, effectiveLabels)
			if err != nil {
				return err
			}

			if repairOnly {
				filtered := make([]service.NodeListItem, 0, len(nodes))
				for _, node := range nodes {
					if node.Repair {
						filtered = append(filtered, node)
					}
				}
				nodes = filtered
			}

			if role != "" {
				filtered := make([]service.NodeListItem, 0, len(nodes))
				for _, node := range nodes {
					if strings.Contains(strings.ToLower(node.ProdRole), strings.ToLower(role)) {
						filtered = append(filtered, node)
					}
				}
				nodes = filtered
			}

			if vcClient, ok := platform.NewVirtualClusterClientFromEnv(); ok {
				clusterUIDs := make([]string, 0, len(nodes))
				for _, node := range nodes {
					if node.ClusterUID != "" {
						clusterUIDs = append(clusterUIDs, node.ClusterUID)
					}
				}

				displayNames, err := vcClient.ResolveDisplayNames(context.Background(), clusterUIDs)
				if err == nil {
					for i := range nodes {
						if displayName, ok := displayNames[nodes[i].ClusterUID]; ok {
							nodes[i].ClusterName = displayName
						}
					}
				}
			}

			displayLimit := limit
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
			output.PrintNodeList(displayNodes, resolvedSelector, len(nodes), start, end, displayLimit, showProdRole)
			return nil
		},
	}

	// 为 get 命令绑定 --labels 标志，允许用户在命令行中追加额外的标签过滤条件
	cmd.Flags().StringVar(&labels, "labels", "", "附加到 profile 选择器上的额外标签选择器")
	cmd.Flags().StringVar(&prod, "prod", "", "Filter by node-role.compute.sensecore.cn/prod, for example ecp-private or ecs")
	cmd.Flags().StringVar(&role, "role", "", "Filter by displayed role, including node-role.sensecore.cn/* fallback roles")
	cmd.Flags().BoolVar(&repairOnly, "repair", false, "Show only cordoned nodes")
	cmd.Flags().IntVarP(&limit, "limit", "l", 100, "Number of nodes to show; use 0 to show all")
	cmd.Flags().BoolVarP(&showAll, "all", "A", false, "Show all nodes")

	return cmd
}

func appendLabelSelector(base string, clause string) string {
	base = strings.TrimSpace(base)
	clause = strings.TrimSpace(clause)
	switch {
	case base == "":
		return clause
	case clause == "":
		return base
	default:
		return base + "," + clause
	}
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
		Short:   "快速检查节点上的 vcluster Pod 和资源占用",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientBegin := time.Now()
			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}
			clientDuration := time.Since(clientBegin)

			nodeService := service.NewNodeService(clientset)

			for i, nodeName := range args {
				details, err := nodeService.Describe(context.Background(), nodeName)
				if err != nil {
					return fmt.Errorf("node %q: %w", nodeName, err)
				}
				if vcClient, ok := platform.NewVirtualClusterClientFromEnv(); ok && strings.TrimSpace(details.VClusterUID) != "" {
					displayNames, err := vcClient.ResolveDisplayNames(context.Background(), []string{details.VClusterUID})
					if err == nil {
						if displayName, ok := displayNames[details.VClusterUID]; ok {
							details.VClusterName = displayName
						}
					}
				}
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

// newNodeCordonCmd 创建 "cordon" 子命令，用于将指定节点置于不可调度 (Cordon) 状态。
// 该操作会阻止新的 Pod 被调度到该节点，同时会给节点打上特定的维修标记。
func newNodeCordonCmd() *cobra.Command {
	// 定义 cordon 命令的结构和行为
	cmd := &cobra.Command{
		Use:   "cordon <node-name>",
		Short: "封锁节点并将其标记为维修状态",
		// 强制要求必须提供 1 个位置参数，即节点名称
		Args:  cobra.ExactArgs(1),
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
		Args:  cobra.ExactArgs(1),
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
