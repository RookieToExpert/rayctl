package platform

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

func (c *VirtualClusterClient) ResolveUsernamesForProfile(ctx context.Context, profileName string, ids []string) (map[string]string, error) {
	profile, ok := c.clientProfileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("platform profile %q not found", profileName)
	}
	return c.resolveUsernamesWithProfile(ctx, profile, ids)
}

// findUsersForProfileFallback narrows a fuzzy lookup on the server before
// applying the same local precedence used by the full-directory lookup.
func (c *VirtualClusterClient) findUsersForProfileFallback(ctx context.Context, profile clientProfile, identifier string) ([]IAMUser, error) {
	users, err := c.listFilteredUsersForProfile(ctx, profile, identifier)
	if cancelErr := canceledIAMUserLookup(ctx, err); cancelErr != nil {
		return nil, cancelErr
	}
	if err == nil {
		if result := matchIAMUsers(users, identifier); len(result) > 0 {
			return result, nil
		}
	} else if !errors.Is(err, errUnsupportedIAMUserFilter) {
		return nil, err
	}

	// Older IAM deployments may reject name filters or return no matches for
	// wildcard/case combinations they do not support. Keep the legacy lookup.
	users, err = c.listUsersForProfile(ctx, profile)
	if err != nil {
		return nil, err
	}
	return matchIAMUsers(users, identifier), nil
}

func matchIAMUsers(users []IAMUser, identifier string) []IAMUser {
	exact := make([]IAMUser, 0)
	prefix := make([]IAMUser, 0)
	contains := make([]IAMUser, 0)
	lowerID := strings.ToLower(identifier)
	for _, user := range users {
		username := strings.TrimSpace(user.Username)
		id := strings.TrimSpace(user.ID)
		name := strings.TrimSpace(user.Name)
		switch {
		case username == identifier || id == identifier:
			exact = append(exact, user)
		case strings.HasPrefix(strings.ToLower(username), lowerID) || strings.HasPrefix(strings.ToLower(id), lowerID) || strings.HasPrefix(strings.ToLower(name), lowerID):
			prefix = append(prefix, user)
		case strings.Contains(strings.ToLower(username), lowerID) || strings.Contains(strings.ToLower(id), lowerID) || strings.Contains(strings.ToLower(name), lowerID):
			contains = append(contains, user)
		}
	}

	result := exact
	if len(result) == 0 {
		result = prefix
	}
	if len(result) == 0 {
		result = contains
	}
	sort.Slice(result, func(i, j int) bool {
		if strings.TrimSpace(result[i].Username) != strings.TrimSpace(result[j].Username) {
			return strings.TrimSpace(result[i].Username) < strings.TrimSpace(result[j].Username)
		}
		return strings.TrimSpace(result[i].ID) < strings.TrimSpace(result[j].ID)
	})
	return result
}

func canceledIAMUserLookup(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}
