package platform

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCloudAuditQueryURL(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	query := CloudAuditQuery{
		Start:        time.Date(2026, 7, 12, 0, 0, 0, 0, location),
		End:          time.Date(2026, 7, 13, 0, 0, 0, 0, location),
		ServiceType:  "ECP",
		ResourceType: "vcjob",
		Limit:        10,
	}
	got := cloudAuditQueryURL(clientProfile{IAMBaseURL: "https://iam.d.pjlab.org.cn"}, query)
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "trail.d.pjlab.org.cn" || parsed.Path != "/cts/data/v1/auditevents" {
		t.Fatalf("unexpected audit URL: %s", got)
	}
	filter := parsed.Query().Get("filter")
	for _, part := range []string{
		"time>='2026-07-12T00:00:00+08:00'",
		"time<='2026-07-13T00:00:00+08:00'",
		"service_type='ECP'",
		"resource_type='vcjob'",
	} {
		if !strings.Contains(filter, part) {
			t.Fatalf("filter %q does not contain %q", filter, part)
		}
	}
	if parsed.Query().Get("trail_type") != "res" || parsed.Query().Get("page_size") != "10" {
		t.Fatalf("unexpected query parameters: %s", parsed.RawQuery)
	}
}

func TestTrailBaseURLFromCloudIAM(t *testing.T) {
	got := trailBaseURLFromIAMBaseURL("https://iam-cloud.d.pjlab.org.cn")
	if got != "https://trail-cloud.d.pjlab.org.cn" {
		t.Fatalf("trail cloud URL = %q", got)
	}
}
