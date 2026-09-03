package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	nodeCmd.AddCommand(newNodeListCmd())
	// 注册子命令：查看节点详情 (包含资源与运行中的 Pod)
	nodeCmd.AddCommand(newNodeDescribeCmd())
	// 注册子命令：封锁节点 (标记不可调度并添加维修标签)
	nodeCmd.AddCommand(newNodeCordonCmd())
	// 注册子命令：解封节点 (恢复可调度并移除维修标签)
	nodeCmd.AddCommand(newNodeUncordonCmd())

	return nodeCmd
}

// newNodeListCmd 创建 "list" 子命令，用于根据 profile 或 label selector 获取节点列表。
func newNodeListCmd() *cobra.Command {
	var longOutput bool
	var showAll bool
	var queueFilter string
	var vcFilter string

	// 定义 get 命令的结构和行为
	cmd := &cobra.Command{
		Use:   "list [profile-or-selector]",
		Short: "通过 profile 或 label selector 列出节点",
		Long: "通过 profile、label selector 或 InternalIP 片段列出节点。\n" +
			"支持直接传入 IP 过滤片段，例如 10.12.14；也支持使用 | 分隔多个片段做或匹配，例如 '10.12|10.140'。",
		Example: strings.Join([]string{
			"  rayctl node list",
			"  rayctl node list ecp",
			"  rayctl node list 'node-role.compute.sensecore.cn/prod=ecs'",
			"  rayctl node list -q queue-d-reserved-a3-llm-share",
			"  rayctl node list -v vc-a3-ailab",
			"  rayctl node list -q queue-d-reserved-a3-llm-share -v vc-a3-ailab",
			"  rayctl node list -A 10.12.14",
			"  rayctl node list -A '10.12|10.140'",
		}, "\n"),
		// 限制最多只能接受 1 个位置参数 (作为 target)
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			queueFilter = strings.TrimSpace(queueFilter)
			vcFilter = strings.TrimSpace(vcFilter)
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

			vcClient, hasVCClient := platform.NewVirtualClusterClientFromEnv()
			if hasVCClient {
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

			if queueFilter != "" {
				if !hasVCClient {
					return fmt.Errorf("--queue requires platform configuration in ~/.rayctl/platform.json")
				}
				resourceService := service.NewSSPResourceService(clientset, vcClient)
				resourceService.SetQueueNodeClientResolver(localQueueVClusterClient)
				queueNodes, err := resourceService.ListQueueNodes(cmd.Context(), queueFilter, "")
				if err != nil {
					return fmt.Errorf("resolve queue %q nodes: %w", queueFilter, err)
				}
				nodes = filterNodeListByQueue(nodes, queueNodes)
			}
			if vcFilter != "" {
				nodes = filterNodeListByVC(nodes, vcFilter)
			}
			resolvedSelector = nodeListFilterDescription(resolvedSelector, queueFilter, vcFilter)

			displayLimit := 100
			if showAll {
				displayLimit = 0
			}

			displayNodes, start, end := limitNodes(nodes, displayLimit)
			enrichNodeListQueuesFromLocalVC(cmd.Context(), displayNodes, 6)

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
	cmd.Flags().StringVarP(&queueFilter, "queue", "q", "", "只显示指定 SSP Queue 下的节点（名称或 UID）")
	cmd.Flags().StringVarP(&vcFilter, "vc", "v", "", "只显示指定 VC 下的节点（名称或 UID）")

	return cmd
}

func filterNodeListByQueue(nodes []service.NodeListItem, queueNodes *service.SSPQueueNodeListResult) []service.NodeListItem {
	if queueNodes == nil {
		return nil
	}
	matchedHosts := make(map[string]struct{}, len(queueNodes.Items)*2)
	for _, item := range queueNodes.Items {
		if host := strings.ToLower(strings.TrimSpace(item.HostName)); host != "" {
			matchedHosts[host] = struct{}{}
		}
		if ip := strings.ToLower(strings.TrimSpace(item.HostIP)); ip != "" {
			matchedHosts[ip] = struct{}{}
		}
	}
	result := make([]service.NodeListItem, 0, len(nodes))
	for _, node := range nodes {
		_, nameMatched := matchedHosts[strings.ToLower(strings.TrimSpace(node.Name))]
		_, ipMatched := matchedHosts[strings.ToLower(strings.TrimSpace(node.InternalIP))]
		if !nameMatched && !ipMatched {
			continue
		}
		node.QueueName = firstNonEmptyString(queueNodes.Queue.Name, node.QueueName)
		result = append(result, node)
	}
	return result
}

func filterNodeListByVC(nodes []service.NodeListItem, identifier string) []service.NodeListItem {
	identifier = strings.ToLower(strings.TrimSpace(identifier))
	if identifier == "" {
		return nodes
	}
	result := make([]service.NodeListItem, 0, len(nodes))
	for _, node := range nodes {
		candidates := []string{
			strings.TrimSpace(node.ClusterName),
			strings.TrimSpace(node.ClusterUID),
		}
		if node.ClusterUID != "" {
			candidates = append(candidates, "vc-"+strings.TrimPrefix(strings.TrimSpace(node.ClusterUID), "vc-"))
		}
		matched := false
		for _, candidate := range candidates {
			if strings.EqualFold(identifier, candidate) {
				matched = true
				break
			}
		}
		if matched {
			result = append(result, node)
		}
	}
	return result
}

func nodeListFilterDescription(selector string, queue string, vc string) string {
	parts := make([]string, 0, 3)
	if strings.TrimSpace(selector) != "" {
		parts = append(parts, selector)
	}
	if strings.TrimSpace(queue) != "" {
		parts = append(parts, "queue="+strings.TrimSpace(queue))
	}
	if strings.TrimSpace(vc) != "" {
		parts = append(parts, "vc="+strings.TrimSpace(vc))
	}
	return strings.Join(parts, ", ")
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
			"  rayctl node check 10.140.214.222 10.140.214.223",
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
			normalizedNames := make([]string, len(args))
			for i, nodeName := range args {
				normalizedNames[i] = normalizeNodeIdentifier(nodeName)
			}
			results := nodeService.DescribeMany(cmd.Context(), normalizedNames, 4)
			if hasVCClient {
				resolveNodeVCNames(cmd.Context(), vcClient, results, 4)
			}
			resolveNodeDescribeQueuesFromLocalVC(cmd.Context(), results, 4)

			var queryErrors []error
			printed := 0
			for i, result := range results {
				if result.Err != nil {
					queryErrors = append(queryErrors, fmt.Errorf("node %q: %w", args[i], result.Err))
					continue
				}
				if printed > 0 {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				if len(args) > 1 {
					fmt.Fprintf(cmd.OutOrStdout(), "===== NODE [%d/%d]: %s =====\n\n", i+1, len(args), args[i])
				}
				output.PrintNodeDescribe(result.Details, debugTiming, clientDuration)
				printed++
			}
			return errors.Join(queryErrors...)
		},
	}

	cmd.Flags().BoolVar(&debugTiming, "debug-timing", false, "Print timing diagnostics for node check")

	return cmd
}

type localVCNodeQueueResult struct {
	VC          string
	QueueByNode map[string]string
	Duration    time.Duration
}

func enrichNodeListQueuesFromLocalVC(ctx context.Context, nodes []service.NodeListItem, maxParallel int) {
	indicesByVC := make(map[string][]int)
	for index, node := range nodes {
		if strings.TrimSpace(node.QueueName) != "" || !isVClusterDisplayName(node.ClusterName) {
			continue
		}
		indicesByVC[node.ClusterName] = append(indicesByVC[node.ClusterName], index)
	}
	lookups := loadLocalVCNodeQueues(ctx, mapKeys(indicesByVC), maxParallel)
	for _, lookup := range lookups {
		for _, index := range indicesByVC[lookup.VC] {
			if queue := lookup.QueueByNode[nodes[index].Name]; queue != "" {
				nodes[index].QueueName = queue
			}
		}
	}
}

func resolveNodeDescribeQueuesFromLocalVC(ctx context.Context, results []service.NodeDescribeQueryResult, maxParallel int) {
	indicesByVC := make(map[string][]int)
	for index, result := range results {
		if result.Err != nil || result.Details == nil || strings.TrimSpace(result.Details.QueueName) != "" || !isVClusterDisplayName(result.Details.VClusterName) {
			continue
		}
		indicesByVC[result.Details.VClusterName] = append(indicesByVC[result.Details.VClusterName], index)
	}
	lookups := loadLocalVCNodeQueues(ctx, mapKeys(indicesByVC), maxParallel)
	for _, lookup := range lookups {
		for _, index := range indicesByVC[lookup.VC] {
			details := results[index].Details
			if queue := lookup.QueueByNode[details.Hostname]; queue != "" {
				details.QueueName = queue
			}
			details.Timings.ResolveQueue = lookup.Duration
		}
	}
}

func loadLocalVCNodeQueues(ctx context.Context, vcNames []string, maxParallel int) []localVCNodeQueueResult {
	return runBoundedQueries(ctx, vcNames, maxParallel, func(parent context.Context, vcName string) localVCNodeQueueResult {
		startedAt := time.Now()
		result := localVCNodeQueueResult{VC: vcName, QueueByNode: make(map[string]string)}
		clientset, err := localQueueVClusterClient(service.SSPQueueItem{VCluster: vcName})
		if err != nil {
			result.Duration = time.Since(startedAt)
			return result
		}
		queryCtx, cancel := context.WithTimeout(parent, 2*time.Second)
		defer cancel()
		nodes, err := clientset.CoreV1().Nodes().List(queryCtx, metav1.ListOptions{ResourceVersion: "0"})
		if err == nil {
			for _, node := range nodes.Items {
				queue := firstNonEmptyString(
					strings.TrimSpace(node.Labels["resource.compute.sensecore.cn/queue-name"]),
					strings.TrimSpace(node.Labels["resource.compute.sensecore.cn/queue-uid"]),
				)
				if queue != "" {
					result.QueueByNode[node.Name] = queue
				}
			}
		}
		result.Duration = time.Since(startedAt)
		return result
	})
}

func isVClusterDisplayName(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(value, "vc-") && !strings.HasPrefix(value, "vc-0")
}

func mapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func resolveNodeVCNames(ctx context.Context, vcClient *platform.VirtualClusterClient, results []service.NodeDescribeQueryResult, maxParallel int) {
	indicesByUID := make(map[string][]int)
	for index, result := range results {
		if result.Err != nil || result.Details == nil {
			continue
		}
		uid := strings.TrimSpace(result.Details.VClusterUID)
		if uid != "" {
			indicesByUID[uid] = append(indicesByUID[uid], index)
		}
	}
	if len(indicesByUID) == 0 {
		return
	}
	if maxParallel < 1 {
		maxParallel = 1
	}

	uids := make(chan string)
	resolved := make(map[string]string, len(indicesByUID))
	resolveDurations := make(map[string]time.Duration, len(indicesByUID))
	var resultMu sync.Mutex
	var workers sync.WaitGroup
	if maxParallel > len(indicesByUID) {
		maxParallel = len(indicesByUID)
	}
	workers.Add(maxParallel)
	for range maxParallel {
		go func() {
			defer workers.Done()
			for uid := range uids {
				startedAt := time.Now()
				resolveCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				resource, err := vcClient.FindResourceByUID(resolveCtx, uid, "virtualClusters")
				cancel()
				name := ""
				if err == nil {
					name = strings.TrimSpace(resource.Name)
				}
				resultMu.Lock()
				resolved[uid] = name
				resolveDurations[uid] = time.Since(startedAt)
				resultMu.Unlock()
			}
		}()
	}
	for uid := range indicesByUID {
		uids <- uid
	}
	close(uids)
	workers.Wait()

	for uid, indices := range indicesByUID {
		for _, index := range indices {
			if resolved[uid] != "" {
				results[index].Details.VClusterName = resolved[uid]
			}
			results[index].Details.Timings.ResolveVC = resolveDurations[uid]
		}
	}
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
		Use:   "cordon <node-name-or-ip> [node-name-or-ip...]",
		Short: "并行封锁一个或多个节点并将其标记为维修状态",
		Example: strings.Join([]string{
			"  rayctl node cordon host-10-140-214-222",
			"  rayctl node cordon 10.140.214.222",
			"  rayctl node cordon 10.140.214.222 10.140.214.223",
		}, "\n"),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. 初始化 Kubernetes 客户端
			// kube.NewClientset 定义于 internal/kube/client.go
			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}

			nodeService := service.NewNodeService(clientset)
			return runNodeMutations(cmd, args, nodeService.Cordon)
		},
	}

	return cmd
}

// newNodeUncordonCmd 创建 "uncordon" 子命令，用于将指定节点恢复为正常可调度状态。
// 该操作会允许新的 Pod 重新被调度到该节点，同时会清除之前打上的维修标记。
func newNodeUncordonCmd() *cobra.Command {
	// 定义 uncordon 命令的结构和行为
	cmd := &cobra.Command{
		Use:   "uncordon <node-name-or-ip> [node-name-or-ip...]",
		Short: "并行解封一个或多个节点并清除维修标签",
		Example: strings.Join([]string{
			"  rayctl node uncordon host-10-140-214-222",
			"  rayctl node uncordon 10.140.214.222",
			"  rayctl node uncordon 10.140.214.222 10.140.214.223",
		}, "\n"),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. 初始化 Kubernetes 客户端
			// kube.NewClientset 定义于 internal/kube/client.go
			clientset, err := kube.NewClientset(kubeconfig)
			if err != nil {
				return err
			}

			nodeService := service.NewNodeService(clientset)
			return runNodeMutations(cmd, args, nodeService.Uncordon)
		},
	}

	return cmd
}

type nodeMutationQueryResult struct {
	identifier string
	result     *service.NodeMutationResult
	err        error
}

func runNodeMutations(cmd *cobra.Command, args []string, mutate func(context.Context, string) (*service.NodeMutationResult, error)) error {
	results := runBoundedQueries(cmd.Context(), args, 4, func(ctx context.Context, identifier string) nodeMutationQueryResult {
		result, err := mutate(ctx, normalizeNodeIdentifier(identifier))
		return nodeMutationQueryResult{identifier: identifier, result: result, err: err}
	})

	mutations := make([]*service.NodeMutationResult, 0, len(results))
	queryErrors := make([]error, 0)
	for _, result := range results {
		if result.err != nil {
			queryErrors = append(queryErrors, fmt.Errorf("node %q: %w", result.identifier, result.err))
			continue
		}
		mutations = append(mutations, result.result)
	}
	if len(mutations) > 0 {
		output.PrintNodeMutationResults(mutations)
	}
	return errors.Join(queryErrors...)
}
