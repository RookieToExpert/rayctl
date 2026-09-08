package platform

import (
	"path/filepath"
	"testing"
)

func TestCurrentMetricsConfigUsesEnvironmentDefaults(t *testing.T) {
	t.Setenv("RAYCTL_METRICS_BASE_URL", "")
	t.Setenv("RAYCTL_METRICS_USERNAME", "")
	t.Setenv("RAYCTL_METRICS_PASSWORD", "")
	client := &VirtualClusterClient{
		currentProfile: "pt",
		profiles: map[string]clientProfile{
			"pt": {Name: "pt", Region: "cn-pj-03", BaseURL: "https://management.pjlab.org.cn", MetricsUsername: "user", MetricsPassword: "pass"},
		},
	}

	config, err := client.CurrentMetricsConfig()
	if err != nil {
		t.Fatalf("CurrentMetricsConfig() error = %v", err)
	}
	if config.Environment != "pt" || config.BaseURL != "k8s://prod-datalake/vmauth-datalake-vm-cms:8427" {
		t.Fatalf("config = %#v", config)
	}
}

func TestMetricsConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "platform.json")
	input := &ConfigSnapshot{
		CurrentProfile: "ailabdev",
		Profiles: map[string]ConfigProfile{
			"ailabdev": {
				AccessKey: "ak", SecretKey: "sk", Region: "cn-pj-01",
				MetricsBaseURL: "https://vm.example", MetricsUsername: "user", MetricsPassword: "pass",
			},
		},
	}
	if err := SaveConfigSnapshot(path, input); err != nil {
		t.Fatalf("SaveConfigSnapshot() error = %v", err)
	}
	loaded, err := LoadConfigSnapshot(path)
	if err != nil {
		t.Fatalf("LoadConfigSnapshot() error = %v", err)
	}
	profile := loaded.Profiles["ailabdev"]
	if profile.MetricsBaseURL != "https://vm.example" || profile.MetricsUsername != "user" || profile.MetricsPassword != "pass" {
		t.Fatalf("metrics profile was not preserved: %#v", profile)
	}
}
