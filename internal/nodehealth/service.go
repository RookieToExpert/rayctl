package nodehealth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	metricsquery "rayctl/internal/metrics"
	"rayctl/internal/nodeexec"
)

const machineTypeLabel = "resource.compute.sensecore.cn/machine-type"

type MetricsQuerier interface {
	Query(context.Context, string, time.Time) ([]metricsquery.RawSeries, error)
}

type Service struct {
	client      kubernetes.Interface
	metrics     MetricsQuerier
	executor    nodeexec.Runner
	environment string
}

func NewService(client kubernetes.Interface, metrics MetricsQuerier, executor nodeexec.Runner, environment string) *Service {
	return &Service{
		client:      client,
		metrics:     metrics,
		executor:    executor,
		environment: strings.ToLower(strings.TrimSpace(environment)),
	}
}

func (s *Service) ResolveNodes(ctx context.Context, identifiers []string) ([]*corev1.Node, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("kubernetes client is unavailable")
	}
	listed, err := s.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	byName := make(map[string]*corev1.Node, len(listed.Items))
	byIP := make(map[string]*corev1.Node, len(listed.Items))
	for index := range listed.Items {
		node := &listed.Items[index]
		byName[strings.ToLower(node.Name)] = node
		for _, address := range node.Status.Addresses {
			if address.Type == corev1.NodeInternalIP && strings.TrimSpace(address.Address) != "" {
				byIP[strings.ToLower(strings.TrimSpace(address.Address))] = node
			}
		}
	}

	result := make([]*corev1.Node, 0, len(identifiers))
	seen := make(map[string]struct{}, len(identifiers))
	missing := make([]string, 0)
	for _, identifier := range identifiers {
		identifier = strings.TrimSpace(identifier)
		if identifier == "" {
			continue
		}
		key := strings.ToLower(identifier)
		node := byName[key]
		if node == nil {
			node = byIP[key]
		}
		if node == nil && net.ParseIP(identifier) != nil {
			node = byName["host-"+strings.ReplaceAll(key, ".", "-")]
		}
		if node == nil {
			missing = append(missing, identifier)
			continue
		}
		if _, exists := seen[node.Name]; exists {
			continue
		}
		seen[node.Name] = struct{}{}
		result = append(result, node.DeepCopy())
	}
	if len(missing) > 0 {
		return result, fmt.Errorf("nodes not found in current HC cluster: %s", strings.Join(missing, ", "))
	}
	return result, nil
}

func (s *Service) Run(ctx context.Context, nodes []*corev1.Node, options Options) (*Result, error) {
	if len(options.Checks) == 0 {
		options.Checks = append([]string(nil), DefaultChecks...)
	}
	if options.MaxParallel <= 0 {
		options.MaxParallel = 4
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.MaxParallel > len(nodes) {
		options.MaxParallel = len(nodes)
	}
	if len(nodes) == 0 {
		return &Result{}, nil
	}

	type indexedResult struct {
		index  int
		result NodeResult
		err    error
	}
	jobs := make(chan int)
	results := make(chan indexedResult, len(nodes))
	var workers sync.WaitGroup
	workers.Add(options.MaxParallel)
	for range options.MaxParallel {
		go func() {
			defer workers.Done()
			for index := range jobs {
				result, err := s.runNode(ctx, nodes[index], options)
				results <- indexedResult{index: index, result: result, err: err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index := range nodes {
			select {
			case jobs <- index:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	output := &Result{Nodes: make([]NodeResult, len(nodes))}
	queryErrors := make([]error, 0)
	completed := 0
	for item := range results {
		output.Nodes[item.index] = item.result
		completed++
		if item.err != nil {
			queryErrors = append(queryErrors, item.err)
		}
	}
	if completed != len(nodes) && ctx.Err() != nil {
		queryErrors = append(queryErrors, ctx.Err())
	}
	output.Summary = summarize(output.Nodes)
	return output, errors.Join(queryErrors...)
}

func (s *Service) runNode(ctx context.Context, node *corev1.Node, options Options) (NodeResult, error) {
	result := NodeResult{
		Node:       node.Name,
		Ready:      readyText(node),
		InternalIP: internalIP(node),
		VCUID:      nodeVCUID(node.Labels),
		Labels: NodeLabels{
			MachineType: strings.TrimSpace(node.Labels[machineTypeLabel]),
			VC:          nodeVCName(node.Labels),
		},
		Checks: make([]CheckResult, len(options.Checks)),
	}

	type checkOutput struct {
		index int
		check CheckResult
		err   error
	}
	checks := make(chan checkOutput, len(options.Checks))
	for index, checkID := range options.Checks {
		go func(index int, checkID string) {
			started := time.Now()
			check, err := s.runCheck(ctx, node, checkID, options)
			check.ID = checkID
			check.DurationMS = time.Since(started).Milliseconds()
			if check.Evidence == nil {
				check.Evidence = map[string]any{}
			}
			if err != nil {
				check.Status = StatusSkip
				check.Message = err.Error()
				check.Evidence = map[string]any{"reason": "execution error", "error": err.Error()}
			}
			checks <- checkOutput{index: index, check: check, err: err}
		}(index, checkID)
	}

	checkErrors := make([]error, 0)
	for range options.Checks {
		item := <-checks
		result.Checks[item.index] = item.check
		if item.err != nil {
			checkErrors = append(checkErrors, fmt.Errorf("node %s check %s: %w", node.Name, item.check.ID, item.err))
		}
	}
	statuses := make([]Status, 0, len(result.Checks))
	for _, check := range result.Checks {
		statuses = append(statuses, check.Status)
	}
	result.Status = WorstStatus(statuses...)
	return result, errors.Join(checkErrors...)
}

func (s *Service) runCheck(ctx context.Context, node *corev1.Node, checkID string, options Options) (CheckResult, error) {
	if options.NoExec && requiresExec(checkID) {
		return skipCheck("--no-exec"), nil
	}
	switch checkID {
	case CheckNodeBasic:
		return checkNodeBasic(node), nil
	case CheckDisk:
		return s.checkDisk(ctx, node, options.Now())
	case CheckDevicePlugin:
		return s.checkDevicePlugin(ctx, node)
	case CheckClockSkew:
		return s.checkClockSkew(ctx, node)
	case CheckKernelErrors:
		return s.checkKernelErrors(ctx, node)
	case CheckMindXNPU:
		return s.checkMindXNPU(ctx, node, options.Now())
	default:
		return CheckResult{}, fmt.Errorf("unknown check %q", checkID)
	}
}

func requiresExec(checkID string) bool {
	switch checkID {
	case CheckDevicePlugin, CheckClockSkew, CheckKernelErrors:
		return true
	default:
		return false
	}
}

func checkNodeBasic(node *corev1.Node) CheckResult {
	ready := nodeReady(node)
	taints := make([]string, 0, len(node.Spec.Taints))
	for _, taint := range node.Spec.Taints {
		value := taint.Key
		if taint.Value != "" {
			value += "=" + taint.Value
		}
		if taint.Effect != "" {
			value += ":" + string(taint.Effect)
		}
		taints = append(taints, value)
	}
	sort.Strings(taints)
	status := StatusOK
	if !ready || node.Spec.Unschedulable {
		status = StatusFail
	}
	message := fmt.Sprintf("Ready=%t，taint=%d，cordon=%t", ready, len(taints), node.Spec.Unschedulable)
	return CheckResult{
		Status:  status,
		Message: message,
		Evidence: map[string]any{
			"ready": ready, "unschedulable": node.Spec.Unschedulable, "taints": taints,
		},
	}
}

func skipCheck(reason string) CheckResult {
	return CheckResult{Status: StatusSkip, Message: reason, Evidence: map[string]any{"reason": reason}}
}

func nodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func readyText(node *corev1.Node) string {
	if nodeReady(node) {
		return "Ready"
	}
	return "NotReady"
}

func internalIP(node *corev1.Node) string {
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			return strings.TrimSpace(address.Address)
		}
	}
	return ""
}

func nodeVCName(labels map[string]string) string {
	for _, key := range []string{
		"cluster.x-k8s.io/vcluster-name",
		"cluster.x-k8s.io/cluster-name",
		"cluster.x-k8s.io/vcluster-namespace",
		"resource.compute.sensecore.cn/vc-uid",
	} {
		if value := strings.TrimSpace(labels[key]); value != "" {
			return value
		}
	}
	return ""
}

func nodeVCUID(labels map[string]string) string {
	if value := strings.TrimSpace(labels["resource.compute.sensecore.cn/vc-uid"]); value != "" {
		return strings.TrimPrefix(value, "vc-")
	}
	value := nodeVCName(labels)
	return strings.TrimPrefix(value, "vc-")
}

func summarize(nodes []NodeResult) Summary {
	summary := Summary{Total: len(nodes)}
	for _, node := range nodes {
		switch node.Status {
		case StatusFail:
			summary.Fail++
		case StatusWarn:
			summary.Warn++
		case StatusSkip:
			summary.Skip++
		default:
			summary.OK++
		}
	}
	return summary
}

func isNotFound(err error) bool {
	return apierrors.IsNotFound(err)
}
