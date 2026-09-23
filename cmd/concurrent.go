package cmd

import (
	"context"

	"rayctl/internal/concurrent"
)

func runBoundedQueries[I any, T any](
	ctx context.Context,
	identifiers []I,
	maxParallel int,
	query func(context.Context, I) T,
) []T {
	return concurrent.Map(ctx, identifiers, maxParallel, query)
}
