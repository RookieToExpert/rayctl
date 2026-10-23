package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"rayctl/internal/platform"
)

func (s *AuthService) findAuthResource(ctx context.Context, resourceType, identifier, bearerToken string) (*platform.StorageVolumeResource, error) {
	if resourceType == "queue" {
		return s.vcClient.FindSSPQueueAuthResource(ctx, identifier)
	}
	return s.findGrantResource(ctx, resourceType, identifier, bearerToken)
}

// A VC synthesized after a forbidden detail response has no verified owning
// profile. IAM bindings can confirm its scope without changing grant/remove.
func (s *AuthService) resolveUnverifiedVCAuth(ctx context.Context, name string, fallback *platform.StorageVolumeResource) (*platform.StorageVolumeResource, []platform.IAMBindingPolicy, []string, error) {
	type candidate struct {
		profile string
		byScope map[string][]platform.IAMBindingPolicy
		err     error
	}
	query := func(ctx context.Context, profile string) candidate {
		policies, err := s.vcClient.ListIAMBindingPoliciesForResourceProfile(ctx, profile, name)
		result := candidate{profile: profile, byScope: map[string][]platform.IAMBindingPolicy{}, err: err}
		for _, policy := range policies {
			scope := normalizeAuthScope(policy.Scope)
			parts := strings.Split(strings.Trim(scope, "/"), "/")
			// Match the complete resource path, excluding inherited/wildcard
			// scopes, similarly named VCs, and VCs in another region/group.
			if len(parts) != 8 || parts[0] != "subscriptions" || parts[1] == "" || strings.ContainsAny(parts[1], "*?") || parts[2] != "resourceGroups" ||
				parts[4] != "regions" || parts[6] != "virtualClusters" || parts[7] != name ||
				parts[3] != fallback.ResourceGroupName || parts[5] != fallback.Region {
				continue
			}
			result.byScope[scope] = append(result.byScope[scope], policy)
		}
		return result
	}
	selectScope := func(result candidate) (*platform.StorageVolumeResource, []platform.IAMBindingPolicy, []string, error) {
		if len(result.byScope) != 1 {
			return fallback, nil, nil, fmt.Errorf("VC %q has matching IAM scopes in multiple tenants; select the intended platform profile", name)
		}
		for scope, policies := range result.byScope {
			resource := *fallback
			resource.RID = scope
			resource.ProfileName = result.profile
			return &resource, policies, nil, nil
		}
		return fallback, nil, nil, fmt.Errorf("VC %q has no matching IAM scope", name)
	}

	// An exact binding in the active tenant takes precedence over same-name
	// resources elsewhere, matching the existing resource lookup preference.
	current := s.vcClient.CurrentProfileName()
	active := query(ctx, current)
	if active.err != nil {
		return fallback, nil, nil, fmt.Errorf("profile %q: %w", current, active.err)
	}
	if len(active.byScope) > 0 {
		return selectScope(active)
	}
	profiles := make([]string, 0)
	for _, profile := range s.vcClient.ProfileNames("") {
		if profile != current {
			profiles = append(profiles, profile)
		}
	}
	results := boundedMap(ctx, profiles, 4, query)
	queryErrors := make([]error, 0)
	matches := make(map[string]candidate)
	for _, result := range results {
		if result.err != nil {
			queryErrors = append(queryErrors, fmt.Errorf("profile %q: %w", result.profile, result.err))
			continue
		}
		for scope := range result.byScope {
			if _, exists := matches[scope]; !exists {
				matches[scope] = result
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return fallback, nil, nil, err
	}
	if len(queryErrors) > 0 {
		return fallback, nil, nil, errors.Join(queryErrors...)
	}
	if len(matches) > 1 {
		matchingProfiles := make([]string, 0, len(matches))
		for _, result := range matches {
			matchingProfiles = append(matchingProfiles, result.profile)
		}
		sort.Strings(matchingProfiles)
		return fallback, nil, nil, fmt.Errorf("VC %q matches IAM scopes in multiple tenants (%s); select the intended platform profile", name, strings.Join(matchingProfiles, ", "))
	}
	for _, result := range matches {
		return selectScope(result)
	}
	resource := *fallback
	resource.RID, resource.ProfileName = "", ""
	return &resource, nil, []string{
		fmt.Sprintf("无法读取 VC %q 的详情以确认归属，且当前环境已配置租户中未找到精确的直接 IAM 授权；这不代表没有上级继承或 Kubernetes RBAC 权限", name),
	}, nil
}
