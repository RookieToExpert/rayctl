package platform

import (
	"context"

	"rayctl/internal/concurrent"
)

type profileQueryResult[T any] struct {
	profile clientProfile
	value   T
	err     error
}

func queryProfiles[T any](ctx context.Context, profiles []clientProfile, query func(context.Context, clientProfile) (T, error)) []profileQueryResult[T] {
	return concurrent.Map(ctx, profiles, 6, func(ctx context.Context, profile clientProfile) profileQueryResult[T] {
		value, err := query(ctx, profile)
		return profileQueryResult[T]{profile: profile, value: value, err: err}
	})
}
