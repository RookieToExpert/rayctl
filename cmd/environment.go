package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"rayctl/internal/kube"
)

const platformEnvironmentEnv = "RAYCTL_PLATFORM_ENVIRONMENT"

var targetEnvironment = "auto"

func prepareEnvironmentSelection() error {
	environment, err := normalizeCLIEnvironment(targetEnvironment)
	if err != nil {
		return err
	}
	if environment == "auto" {
		environment = inferEnvironmentFromKubeconfigPath(kube.ResolvedKubeconfigPath(kubeconfig))
	}
	if environment == "" {
		return os.Unsetenv(platformEnvironmentEnv)
	}
	return os.Setenv(platformEnvironmentEnv, environment)
}

func normalizeCLIEnvironment(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto", "current", "default":
		return "auto", nil
	case "d":
		return "d", nil
	case "p", "pt":
		return "pt", nil
	case "cloud", "dcloud":
		return "dcloud", nil
	case "all":
		return "all", nil
	default:
		return "", fmt.Errorf("unsupported environment %q, expected auto, d, pt, dcloud, or all", value)
	}
}

func effectiveEnvironmentSelection() string {
	environment, err := normalizeCLIEnvironment(targetEnvironment)
	if err != nil {
		return ""
	}
	if environment != "auto" {
		return environment
	}
	return inferEnvironmentFromKubeconfigPath(kube.ResolvedKubeconfigPath(kubeconfig))
}

func explicitEnvironmentFilter() string {
	environment, err := normalizeCLIEnvironment(targetEnvironment)
	if err != nil || environment == "auto" || environment == "all" {
		return ""
	}
	return environment
}

func selectedSSPRegion() (string, error) {
	switch effectiveEnvironmentSelection() {
	case "", "all":
		return "", nil
	case "d":
		return "cn-pj-01", nil
	case "pt":
		return "cn-pj-03", nil
	case "dcloud":
		return "", fmt.Errorf("SSP is unavailable in dcloud; use -e d or -e pt")
	default:
		return "", nil
	}
}

// Exact SSP lookups prefer the environment inferred from kubeconfig, then let
// the platform client fall back across D/PT. Explicit -e remains strict.
func selectedSSPRegionForLookup() (string, error) {
	environment, err := normalizeCLIEnvironment(targetEnvironment)
	if err != nil {
		return "", err
	}
	if environment == "auto" || environment == "all" {
		return "", nil
	}
	return selectedSSPRegion()
}

func inferEnvironmentFromKubeconfigPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	for _, candidate := range filepath.SplitList(path) {
		if environment := inferEnvironmentFromSingleKubeconfigPath(candidate); environment != "" {
			return environment
		}
	}
	return ""
}

func inferEnvironmentFromSingleKubeconfigPath(path string) string {
	cleaned := filepath.Clean(path)
	base := strings.ToLower(filepath.Base(cleaned))
	parts := strings.FieldsFunc(strings.ToLower(filepath.ToSlash(cleaned)), func(r rune) bool { return r == '/' })
	hasPart := func(values ...string) bool {
		for _, part := range parts {
			for _, value := range values {
				if part == value {
					return true
				}
			}
		}
		return false
	}

	switch {
	case base == "kubeconfigc", strings.Contains(base, "dcloud"), strings.Contains(base, "cloud"), hasPart("dcloud", "cloud"):
		return "dcloud"
	case base == "kubeconfigpt", strings.HasSuffix(base, "-pt"), strings.HasSuffix(base, "_pt"), hasPart("pt"):
		return "pt"
	case base == "kubeconfig", hasPart("d"):
		return "d"
	default:
		return ""
	}
}
