package platform

import "testing"

func TestResolveProfileNameUsesMatchingTenantEnvironment(t *testing.T) {
	client := &VirtualClusterClient{
		currentProfile: "ailabdev",
		profiles: map[string]clientProfile{
			"ailabdev": {
				Name:       "ailabdev",
				BaseURL:    "https://management.d.pjlab.org.cn",
				IAMBaseURL: "https://iam.d.pjlab.org.cn",
				Region:     "cn-pj-01",
			},
			"ailabdev-pt": {
				Name:       "ailabdev-pt",
				BaseURL:    "https://management.pjlab.org.cn",
				IAMBaseURL: "https://iam-api.pjlab.org.cn",
				Region:     "cn-pj-03",
			},
			"pjailab-dcloud": {
				Name:       "pjailab-dcloud",
				BaseURL:    "https://management-cloud.d.pjlab.org.cn",
				IAMBaseURL: "https://iam-cloud.d.pjlab.org.cn",
				Region:     "cn-pj-02",
			},
		},
	}

	for input, want := range map[string]string{
		"":       "ailabdev",
		"d":      "ailabdev",
		"p":      "ailabdev-pt",
		"pt":     "ailabdev-pt",
		"dcloud": "pjailab-dcloud",
	} {
		got, err := client.ResolveProfileName(input)
		if err != nil {
			t.Fatalf("ResolveProfileName(%q) error = %v", input, err)
		}
		if got != want {
			t.Fatalf("ResolveProfileName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSelectProfileForProcessDoesNotRequireConfigRewrite(t *testing.T) {
	client := &VirtualClusterClient{
		currentProfile: "ailabdev",
		profiles: map[string]clientProfile{
			"ailabdev": {
				Name:       "ailabdev",
				BaseURL:    "https://management.d.pjlab.org.cn",
				IAMBaseURL: "https://iam.d.pjlab.org.cn",
				Region:     "cn-pj-01",
			},
			"pjailab-dcloud": {
				Name:       "pjailab-dcloud",
				BaseURL:    "https://management-cloud.d.pjlab.org.cn",
				IAMBaseURL: "https://iam-cloud.d.pjlab.org.cn",
				Region:     "cn-pj-02",
			},
		},
	}

	name, err := client.SelectProfileForProcess("dcloud")
	if err != nil {
		t.Fatalf("SelectProfileForProcess(dcloud) error = %v", err)
	}
	if name != "pjailab-dcloud" || client.CurrentProfileName() != "pjailab-dcloud" {
		t.Fatalf("selected profile = %q current = %q", name, client.CurrentProfileName())
	}
	if got := client.CurrentIAMBaseURL(); got != "https://iam-cloud.d.pjlab.org.cn" {
		t.Fatalf("CurrentIAMBaseURL() = %q", got)
	}
}
