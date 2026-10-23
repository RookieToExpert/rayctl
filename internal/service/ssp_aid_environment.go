package service

import (
	"context"
	"errors"
)

// A located AID is useful even when unrelated scopes cannot be read. Keep the
// incomplete scope visible without turning that successful lookup into failure.
func QueryAIDEnvironments(ctx context.Context, targets []HCEnvironment, identifier string, query func(context.Context, HCEnvironment) (*SSPAIDGetResult, error)) (*SSPAIDGetResult, error) {
	result, err := QueryHCEnvironments(ctx, targets, identifier, query, func(err error) bool {
		var missing *AIDNotFoundError
		return errors.As(err, &missing)
	})
	if result != nil && err != nil && ctx.Err() == nil {
		result.QueryIncomplete = true
		result.Warnings = append(result.Warnings, partialSSPQueryWarning(err))
		return result, nil
	}
	if err != nil {
		return result, singleLineQueryError{err}
	}
	return result, nil
}

func (r *SSPAIDGetResult) IsQueryComplete() bool { return r == nil || !r.QueryIncomplete }
