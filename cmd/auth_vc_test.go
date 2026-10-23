package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestVCAuthCheckWarnsWhenScopeCannotBeVerified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/virtualClusters/") {
			http.Error(w, "detail forbidden", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/iam/authz/v1/bindingPolicies" {
			fmt.Fprint(w, `{"policies":[]}`)
			return
		}
		t.Errorf("unexpected request: %s", r.URL)
		http.NotFound(w, r)
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "platform.json")
	config := fmt.Sprintf(`{"current_profile":"test","profiles":{"test":{"access_key":"ak","secret_key":"sk","subscription_id":"sub","base_url":%q,"iam_base_url":%q}}}`, server.URL, server.URL)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAYCTL_PLATFORM_CONFIG", configPath)
	t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
	root := &cobra.Command{Use: "rayctl", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringP("output", "o", "table", "")
	root.AddCommand(newAuthCmd())
	installJSONCommands(root)
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"auth", "check", "vc", "vc-target", "-o", "json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Scope    *string  `json:"scope"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err, out.String())
	}
	if result.Scope != nil || len(result.Warnings) == 0 || !strings.Contains(errOut.String(), "vc-target") || !strings.Contains(errOut.String(), "无法读取") {
		t.Fatalf("unverified scope was presented without warning: stdout=%s stderr=%s", out.String(), errOut.String())
	}
}
