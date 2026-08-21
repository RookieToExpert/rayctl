package cmd

import (
	"path/filepath"
	"testing"
)

func TestInferEnvironmentFromKubeconfigPath(t *testing.T) {
	tests := map[string]string{
		"/home/ecs-user/kubeconfig":            "d",
		"/home/ecs-user/kubeconfigpt":          "pt",
		"/home/ecs-user/kubeconfigc":           "dcloud",
		"/home/ecs-user/D/vc-a3":               "d",
		"/home/ecs-user/PT/vc-t":               "pt",
		"/home/ecs-user/dcloud/vc-cloud":       "dcloud",
		"/tmp/custom-kubernetes-configuration": "",
	}
	for path, want := range tests {
		if got := inferEnvironmentFromKubeconfigPath(path); got != want {
			t.Errorf("inferEnvironmentFromKubeconfigPath(%q) = %q, want %q", path, got, want)
		}
	}
	pathList := filepath.Join("/tmp", "custom") + string(filepath.ListSeparator) + "/home/ecs-user/kubeconfigpt"
	if got := inferEnvironmentFromKubeconfigPath(pathList); got != "pt" {
		t.Fatalf("inferEnvironmentFromKubeconfigPath(path list) = %q, want pt", got)
	}
}

func TestNormalizeCLIEnvironment(t *testing.T) {
	for input, want := range map[string]string{
		"": "auto", "auto": "auto", "d": "d", "p": "pt", "pt": "pt", "cloud": "dcloud", "dcloud": "dcloud", "all": "all",
	} {
		got, err := normalizeCLIEnvironment(input)
		if err != nil || got != want {
			t.Fatalf("normalizeCLIEnvironment(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := normalizeCLIEnvironment("unknown"); err == nil {
		t.Fatal("normalizeCLIEnvironment(unknown) unexpectedly succeeded")
	}
}

func TestSelectedSSPRegionForLookup(t *testing.T) {
	previous := targetEnvironment
	t.Cleanup(func() { targetEnvironment = previous })

	targetEnvironment = "auto"
	if got, err := selectedSSPRegionForLookup(); err != nil || got != "" {
		t.Fatalf("auto exact lookup region = %q, %v; want cross-region fallback", got, err)
	}
	targetEnvironment = "pt"
	if got, err := selectedSSPRegionForLookup(); err != nil || got != "cn-pj-03" {
		t.Fatalf("pt exact lookup region = %q, %v; want cn-pj-03", got, err)
	}
	targetEnvironment = "dcloud"
	if _, err := selectedSSPRegionForLookup(); err == nil {
		t.Fatal("dcloud exact SSP lookup unexpectedly succeeded")
	}
}
