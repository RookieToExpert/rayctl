package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"k8s.io/client-go/kubernetes"
)

type HCEnvironment struct {
	Name, Region, Kubeconfig string
	Client                   kubernetes.Interface
	Err                      error
}

// QueryHCEnvironments retains failures even when another environment matches.
// A complete empty search is distinct from an unreadable or cancelled scope.
func QueryHCEnvironments[T any](ctx context.Context, targets []HCEnvironment, identifier string, query func(context.Context, HCEnvironment) (*T, error), missing func(error) bool) (*T, error) {
	var matches []*T
	var matched, searched []string
	var failures []error
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		searched = append(searched, target.Name+"/"+target.Region)
		if target.Err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", target.Name, target.Err))
			continue
		}
		result, err := query(ctx, target)
		if err != nil && !missing(err) {
			failures = append(failures, fmt.Errorf("%s: %w", target.Name, err))
		}
		if result != nil {
			matches = append(matches, result)
			matched = append(matched, target.Name)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("%q matches multiple environments (%s); use -e d or -e pt", identifier, strings.Join(matched, ", "))
	}
	if len(matches) == 1 {
		return matches[0], errors.Join(failures...)
	}
	if len(failures) > 0 {
		return nil, fmt.Errorf("查询 %q 未完整完成（范围 %s）: %w", identifier, strings.Join(searched, ", "), errors.Join(failures...))
	}
	return nil, fmt.Errorf("%q 未找到（已查询 %s）", identifier, strings.Join(searched, ", "))
}
