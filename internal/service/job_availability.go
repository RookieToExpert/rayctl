package service

import (
	"context"
	"errors"
)

var ErrPTATEUnavailable = errors.New("PT 环境没有 ATE 服务，请用 ait")

// This marker is set only when every probed VC Job endpoint returned 404.
// A successful empty list or a different backend error prevents the marker.
type ateEndpointsUnavailableError struct{ cause error }

func (e *ateEndpointsUnavailableError) Error() string { return e.cause.Error() }
func (e *ateEndpointsUnavailableError) Unwrap() error { return e.cause }

func (s *JobService) findPTPlatformJobIdentity(ctx context.Context, identifier string) (*jobIdentity, error) {
	clusters, inventoryErr := s.vcClient.ListLookupVirtualClusters(ctx)
	identity, err := s.findPlatformJobIdentityInClusters(ctx, identifier, clusters)
	if err == nil {
		return identity, nil
	}
	var unavailable *ateEndpointsUnavailableError
	if inventoryErr == nil && ctx.Err() == nil && errors.As(err, &unavailable) {
		return nil, ErrPTATEUnavailable
	}
	var ambiguous *ambiguousJobMatchError
	if errors.As(err, &ambiguous) {
		return nil, err
	}
	return nil, jobLookupFailure(identifier, inventoryErr, err)
}
