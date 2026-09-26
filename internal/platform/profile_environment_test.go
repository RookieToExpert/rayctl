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

func TestProfileNamesFiltersCurrentEnvironment(t *testing.T) {
	client := &VirtualClusterClient{
		currentProfile: "tenant-a-d",
		profiles: map[string]clientProfile{
			"tenant-a-d":  {Name: "tenant-a-d", BaseURL: "https://management.d.pjlab.org.cn", Region: "cn-pj-01"},
			"tenant-b-d":  {Name: "tenant-b-d", BaseURL: "https://management.d.pjlab.org.cn", Region: "cn-pj-01"},
			"tenant-a-pt": {Name: "tenant-a-pt", BaseURL: "https://management.pjlab.org.cn", Region: "cn-pj-03"},
		},
	}
	got := client.ProfileNames("")
	if len(got) != 2 || got[0] != "tenant-a-d" || got[1] != "tenant-b-d" {
		t.Fatalf("ProfileNames(current) = %#v", got)
	}
	if all := client.ProfileNames("all"); len(all) != 3 {
		t.Fatalf("ProfileNames(all) = %#v", all)
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

func TestSelectProfileNameForProcessUsesExactResourceProfile(t *testing.T) {
	client := &VirtualClusterClient{
		currentProfile: "ailabdev",
		profiles: map[string]clientProfile{
			"ailabdev": {
				Name:       "ailabdev",
				IAMBaseURL: "https://iam.d.pjlab.org.cn",
			},
			"pjailab-dcloud": {
				Name:       "pjailab-dcloud",
				IAMBaseURL: "https://iam-cloud.d.pjlab.org.cn",
			},
		},
	}

	name, err := client.SelectProfileNameForProcess("pjailab-dcloud")
	if err != nil {
		t.Fatalf("SelectProfileNameForProcess() error = %v", err)
	}
	if name != "pjailab-dcloud" || client.CurrentProfileName() != "pjailab-dcloud" {
		t.Fatalf("selected profile = %q current = %q", name, client.CurrentProfileName())
	}
	if got := client.CurrentIAMBaseURL(); got != "https://iam-cloud.d.pjlab.org.cn" {
		t.Fatalf("CurrentIAMBaseURL() = %q", got)
	}
}

func TestConfiguredSSPRegionsKeepsCurrentTenantAcrossDAndPT(t *testing.T) {
	client := &VirtualClusterClient{
		currentProfile: "ailabdev",
		profiles: map[string]clientProfile{
			"ailabdev": {
				Name:      "ailabdev",
				AccessKey: "shared-ak",
				Region:    "cn-pj-01",
			},
			"ailabdev-pt": {
				Name:      "ailabdev-pt",
				AccessKey: "shared-ak",
				Region:    "cn-pj-03",
			},
			"pjailab-dcloud": {
				Name:      "pjailab-dcloud",
				AccessKey: "other-ak",
				Region:    "cn-pj-02",
			},
		},
	}

	regions := client.ConfiguredSSPRegions()
	if len(regions) != 2 || regions[0] != "cn-pj-01" || regions[1] != "cn-pj-03" {
		t.Fatalf("ConfiguredSSPRegions() = %#v", regions)
	}
}

func TestNormalizeIAMBaseURLForPT(t *testing.T) {
	tests := map[string]string{
		"":                             "https://iam-api.pjlab.org.cn",
		"https://iam.pjlab.org.cn":     "https://iam-api.pjlab.org.cn",
		"https://iam-api.pjlab.org.cn": "https://iam-api.pjlab.org.cn",
		"https://custom.example.com":   "https://custom.example.com",
	}
	for input, want := range tests {
		if got := normalizeIAMBaseURLForRegion(input, "cn-pj-03"); got != want {
			t.Errorf("normalizeIAMBaseURLForRegion(%q) = %q, want %q", input, got, want)
		}
	}
	if got := normalizeIAMBaseURLForRegion("https://iam.pjlab.org.cn", "cn-pj-01"); got != "https://iam.pjlab.org.cn" {
		t.Errorf("D IAM URL unexpectedly changed to %q", got)
	}
}

func TestSigninURLFromPTIAMBaseURL(t *testing.T) {
	if got := signinURLFromIAMBaseURL("https://iam-api.pjlab.org.cn"); got != "https://signin.pjlab.org.cn/oauth2/token" {
		t.Fatalf("PT signin URL = %q", got)
	}
	if got := signinURLFromIAMBaseURL("https://iam.d.pjlab.org.cn"); got != "https://signin.d.pjlab.org.cn/oauth2/token" {
		t.Fatalf("D signin URL = %q", got)
	}
	if got := signinURLFromIAMBaseURL("https://iam-api.example.com"); got != "https://signin-api.example.com/oauth2/token" {
		t.Fatalf("custom signin URL = %q", got)
	}
}

func TestApplyProcessEnvironmentSelection(t *testing.T) {
	t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "pt")
	client := &VirtualClusterClient{
		currentProfile: "ailabdev",
		profiles: map[string]clientProfile{
			"ailabdev":    {Name: "ailabdev", Region: "cn-pj-01", BaseURL: "https://management.d.pjlab.org.cn"},
			"ailabdev-pt": {Name: "ailabdev-pt", Region: "cn-pj-03", BaseURL: "https://management.pjlab.org.cn"},
		},
	}
	selected, ok := applyProcessEnvironmentSelection(client)
	if !ok || selected.CurrentProfileName() != "ailabdev-pt" {
		t.Fatalf("selected profile = %q, ok=%v", selected.CurrentProfileName(), ok)
	}
}
