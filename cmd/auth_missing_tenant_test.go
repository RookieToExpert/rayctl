package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"rayctl/pkg/output"
)

func TestAuthAllTenantsReportsAbsentUserAndKeepsLookupErrors(t *testing.T) {
	for _, groupError := range []bool{false, true} {
		t.Run(fmt.Sprint(groupError), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.Header.Get("Authorization"), `accesskey="absent"`) {
					fmt.Fprint(w, `{"users":[]}`)
					return
				}
				switch {
				case strings.HasSuffix(r.URL.Path, "/getUsers"):
					fmt.Fprint(w, `{"users":[{"id":"u","username":"person","tenant_code":"good","source":"local"}]}`)
				case strings.HasSuffix(r.URL.Path, ":getGroups"):
					if groupError {
						http.Error(w, "group not found", 404)
					} else {
						fmt.Fprint(w, `{"groups":[]}`)
					}
				case strings.HasSuffix(r.URL.Path, "bindingPolicies"):
					fmt.Fprint(w, `{"policies":[]}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			profiles := map[string]any{}
			for _, name := range []string{"present", "absent"} {
				profiles[name] = map[string]any{"access_key": name, "secret_key": "sk", "region": "cn-pj-01", "iam_base_url": server.URL, "base_url": server.URL}
			}
			data, _ := json.Marshal(map[string]any{"current_profile": "present", "profiles": profiles})
			path := filepath.Join(t.TempDir(), "platform.json")
			os.WriteFile(path, data, 0600)
			t.Setenv("RAYCTL_PLATFORM_CONFIG", path)
			t.Setenv("RAYCTL_PLATFORM_ENVIRONMENT", "")
			oldEnv := targetEnvironment
			targetEnvironment = "d"
			t.Cleanup(func() { targetEnvironment = oldEnv })
			cmd := &cobra.Command{}
			cmd.SetContext(context.Background())
			var stderr bytes.Buffer
			cmd.SetErr(&stderr)
			session := output.BeginJSON()
			defer output.EndJSON()
			err := runAuthCheckUser(cmd, []string{"person"}, false, "", true)
			if (err != nil) != groupError {
				t.Fatal(err)
			}
			if len(session.Warnings) != 1 || !strings.Contains(session.Warnings[0], "租户 absent: 该租户无此用户") {
				t.Fatal(session.Warnings, stderr.String())
			}
			if groupError && !strings.Contains(err.Error(), "group not found") {
				t.Fatal(err)
			}
		})
	}
}
