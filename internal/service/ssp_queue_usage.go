package service

import (
	"context"
	"errors"
	"fmt"

	"rayctl/internal/platform"
)

const (
	queueUsageScopeSharedVC = "vc_shared_candidate_pool"
	queueUsageScopeNodes    = "queue_nodes"
)

func (r *SSPQueueNodeUsageResult) IsQueryComplete() bool {
	return r != nil && r.QuotaError == "" && r.NodeUsageError == ""
}

// Keep the quota and node queries independent: a failure on one side must not
// hide the other side, or turn an unknown quota into apparently free capacity.
func (s *SSPResourceService) getQueueNodeUsageWithQuota(ctx context.Context, queue SSPQueueItem) (*SSPQueueNodeUsageResult, error) {
	quotaQueue := queue
	quotaResult := asyncCall(ctx, func(ctx context.Context) (*platform.SSPQueueQuota, error) {
		return s.platform.GetSSPQueueQuota(ctx, quotaQueue.Profile, quotaQueue.Subscription, quotaQueue.ResourceGroup, quotaQueue.Region, quotaQueue.Cluster, quotaQueue.Name)
	})
	result, nodeErr := s.getQueueNodeUsage(ctx, &queue)
	quota := <-quotaResult
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var errs []error
	if nodeErr != nil {
		errs = append(errs, &queueUsageSourceError{source: "节点用量查询", err: nodeErr})
	}
	if quota.Err != nil {
		errs = append(errs, &queueUsageSourceError{source: "队列配额查询", err: quota.Err})
	}
	if result == nil && quota.Value == nil {
		return nil, errors.Join(errs...)
	}
	if result == nil {
		result = &SSPQueueNodeUsageResult{Queue: queue, SharedVCPool: queueUsesSharedVCNodes(queue)}
	}
	result.Quota = quota.Value
	result.NodeCount = len(result.Items)
	result.Scope = queueUsageScopeNodes
	if result.SharedVCPool {
		result.Scope = queueUsageScopeSharedVC
	}
	if nodeErr != nil {
		result.NodeUsageError = (&queueUsageSourceError{source: "节点用量查询", err: nodeErr}).Error()
	}
	if quota.Err != nil {
		result.QuotaError = (&queueUsageSourceError{source: "队列配额查询", err: quota.Err}).Error()
	}
	return result, errors.Join(errs...)
}

type queueUsageSourceError struct {
	source string
	err    error
}

func (e *queueUsageSourceError) Error() string {
	var status *platform.HTTPStatusError
	if errors.As(e.err, &status) {
		return fmt.Sprintf("%s失败（HTTP %d）", e.source, status.StatusCode)
	}
	return fmt.Sprintf("%s失败：%v", e.source, e.err)
}

func (e *queueUsageSourceError) Unwrap() error { return e.err }
