package service

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestQueryHCEnvironmentScopes(t *testing.T) {
	missing := errors.New("missing")
	backend := errors.New("permission denied")
	for _, tt := range []struct {
		name      string
		values    map[string]string
		errs      map[string]error
		want      string
		errorText string
	}{
		{name: "PT hit", values: map[string]string{"pt": "found"}, want: "found"},
		{name: "missing", errorText: "d/cn-pj-01, pt/cn-pj-03"},
		{name: "partial", values: map[string]string{"pt": "found"}, errs: map[string]error{"d": backend}, want: "found", errorText: "permission denied"},
		{name: "ambiguous", values: map[string]string{"pt": "a", "d": "b"}, errorText: "multiple environments"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var visited []string
			got, err := QueryHCEnvironments(context.Background(), []HCEnvironment{{Name: "d", Region: "cn-pj-01"}, {Name: "pt", Region: "cn-pj-03"}}, "example", func(ctx context.Context, env HCEnvironment) (*string, error) {
				visited = append(visited, env.Name)
				if value, ok := tt.values[env.Name]; ok {
					return &value, nil
				}
				if err := tt.errs[env.Name]; err != nil {
					return nil, err
				}
				return nil, missing
			}, func(err error) bool { return errors.Is(err, missing) })
			if strings.Join(visited, ",") != "d,pt" {
				t.Fatal(visited)
			}
			if tt.want != "" && (got == nil || *got != tt.want) {
				t.Fatalf("got %v err %v", got, err)
			}
			if tt.errorText != "" && (err == nil || !strings.Contains(err.Error(), tt.errorText)) {
				t.Fatalf("error %v", err)
			}
			if tt.errorText == "" && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestQueryHCEnvironmentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := QueryHCEnvironments(ctx, []HCEnvironment{{Name: "d"}}, "x", func(context.Context, HCEnvironment) (*string, error) {
		t.Fatal("queried after cancellation")
		return nil, nil
	}, func(error) bool { return false })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
