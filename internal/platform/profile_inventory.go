package platform

import (
	"context"
	"errors"
	"fmt"
)

// Inventory reads query every configured tenant at the selected endpoint and
// region. Preserve successful pages and report failures instead of silently
// presenting an incomplete inventory as complete.
func listProfileInventory[T any](ctx context.Context, profiles []clientProfile, kind string, read func(context.Context, clientProfile) ([]T, error), normalize func(*T, clientProfile), key func(T) string) ([]T, error) {
	items := make([]T, 0)
	seen := make(map[string]bool)
	errs := make([]error, 0)
	if len(profiles) == 0 {
		return items, fmt.Errorf("no platform profile available for %s inventory", kind)
	}
	for _, query := range queryProfiles(ctx, profiles, read) {
		if query.err != nil {
			errs = append(errs, fmt.Errorf("%s profile %q: %w", kind, query.profile.Name, query.err))
		}
		for _, item := range query.value {
			normalize(&item, query.profile)
			identity := key(item)
			if seen[identity] {
				continue
			}
			seen[identity] = true
			items = append(items, item)
		}
	}
	if err := ctx.Err(); err != nil {
		return items, err
	}
	return items, errors.Join(errs...)
}
