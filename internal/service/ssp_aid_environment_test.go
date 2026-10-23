package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"rayctl/internal/platform"
)

func forbiddenWorkspace(name string) error {
	return &SSPWorkspaceQueryError{Workspace: name, Region: "cn-pj-01", Cause: &platform.HTTPStatusError{StatusCode: 403, URL: "https://private.test/long/resource/url", Body: `{"message":"PermissionDenied"}`}}
}

func TestAIDPartialLookupSucceedsWithOneCompactWarning(t *testing.T) {
	for _, sameEnvironment := range []bool{false, true} {
		result, err := QueryAIDEnvironments(context.Background(), []HCEnvironment{{Name: "d"}, {Name: "pt"}}, "dev-demo", func(_ context.Context, env HCEnvironment) (*SSPAIDGetResult, error) {
			failure := errors.Join(forbiddenWorkspace("skip-webhook-test"), forbiddenWorkspace("skip-hostpath-webhook"))
			if env.Name == "d" {
				if sameEnvironment {
					return &SSPAIDGetResult{Name: "dev-demo", Environment: "d"}, failure
				}
				return nil, failure
			}
			if sameEnvironment {
				return nil, &AIDNotFoundError{Identifier: "dev-demo"}
			}
			return &SSPAIDGetResult{Name: "dev-demo", Environment: "pt"}, nil
		})
		if err != nil || result == nil || !result.QueryIncomplete || len(result.Warnings) != 1 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		warning := result.Warnings[0]
		if strings.ContainsAny(warning, "\r\n") || strings.Contains(warning, "https://") || strings.Contains(warning, "PermissionDenied") || !strings.Contains(warning, "skip-webhook-test") || !strings.Contains(warning, "skip-hostpath-webhook") {
			t.Fatal(warning)
		}
	}
}

func TestAIDMissingDistinguishesCompleteAndInaccessibleScopes(t *testing.T) {
	for _, partial := range []bool{false, true} {
		result, err := QueryAIDEnvironments(context.Background(), []HCEnvironment{{Name: "d", Region: "cn-pj-01"}, {Name: "pt", Region: "cn-pj-03"}}, "missing", func(_ context.Context, env HCEnvironment) (*SSPAIDGetResult, error) {
			if partial && env.Name == "d" {
				return nil, forbiddenWorkspace("skip-webhook-test")
			}
			return nil, &AIDNotFoundError{Identifier: "missing"}
		})
		if result != nil || err == nil || !strings.Contains(err.Error(), "d/cn-pj-01, pt/cn-pj-03") || strings.Contains(err.Error(), "https://") {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if strings.Contains(err.Error(), "未完整完成") != partial {
			t.Fatalf("partial=%v err=%v", partial, err)
		}
	}
}

func TestAIDPartialResultsDoNotHideAmbiguityOrCancellation(t *testing.T) {
	targets := []HCEnvironment{{Name: "d"}, {Name: "pt"}}
	result, err := QueryAIDEnvironments(context.Background(), targets, "same", func(context.Context, HCEnvironment) (*SSPAIDGetResult, error) {
		return &SSPAIDGetResult{Name: "same"}, nil
	})
	if result != nil || err == nil || !strings.Contains(err.Error(), "multiple environments") {
		t.Fatalf("result=%v err=%v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result, err = QueryAIDEnvironments(ctx, targets, "same", func(context.Context, HCEnvironment) (*SSPAIDGetResult, error) {
		cancel()
		return &SSPAIDGetResult{Name: "same"}, forbiddenWorkspace("other")
	})
	if result != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("result=%v err=%v", result, err)
	}
}

func TestWorkspaceErrorRetainsTypedCauseWithoutPrintingURL(t *testing.T) {
	err := forbiddenWorkspace("skip-webhook-test")
	var status *platform.HTTPStatusError
	if !errors.As(err, &status) || status.StatusCode != 403 || strings.Contains(err.Error(), "https://") {
		t.Fatal(err)
	}
}
