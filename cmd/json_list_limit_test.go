package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestJSONInventoryListsReportRequestedLimit(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprintf("empty=%v", empty), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/rmh/v1/resources:page" {
					fmt.Fprint(w, `{"resources":[{"name":"ws-test","type":"compute.ssp.v1.workspace","rid":"/subscriptions/sub/resourceGroups/default/regions/cn-pj-03/workspaces/ws-test"}],"total_size":1}`)
					return
				}
				key := "training_jobs"
				if strings.HasSuffix(r.URL.Path, "/airs") {
					key = "airs"
				} else if !strings.HasSuffix(r.URL.Path, "/trainingJobs") {
					t.Errorf("unexpected request: %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				rows := []map[string]any{}
				if !empty {
					for n := 0; n < 6; n++ {
						rows = append(rows, map[string]any{"name": fmt.Sprintf("job-%d", n), "uid": fmt.Sprintf("uid-%d", n), "status": map[string]any{"create_time": now, "state": "RUNNING"}})
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{key: rows, "total_size": len(rows)})
			}))
			defer server.Close()
			configureScopedWorkloadTest(t, server.URL)
			for _, command := range []string{"ait", "air"} {
				for _, test := range []struct {
					name string
					args []string
					want int
				}{
					{name: "default", want: 50},
					{name: "short limit", args: []string{"-n", "3"}, want: 3},
					{name: "long limit", args: []string{"--limit", "7"}, want: 7},
					{name: "window default", args: []string{"--since", "24h"}, want: 5000},
					{name: "window explicit", args: []string{"-A", "-n", "3"}, want: 3},
				} {
					if command == "air" && strings.HasPrefix(test.name, "window") {
						continue
					}
					t.Run(command+"/"+test.name, func(t *testing.T) {
						root := &cobra.Command{Use: "rayctl", SilenceErrors: true, SilenceUsage: true}
						root.PersistentFlags().StringP("output", "o", "table", "")
						root.AddCommand(newAITCmd(), newAIRCmd())
						installJSONCommands(root)
						args := []string{command}
						if command == "air" {
							args = append(args, "job")
						}
						args = append(args, "list", "-o", "json")
						args = append(args, test.args...)
						var out bytes.Buffer
						root.SetOut(&out)
						root.SetArgs(args)
						if err := root.Execute(); err != nil {
							t.Fatal(err)
						}
						var data struct {
							Items   []any `json:"items"`
							Summary struct {
								Requested int `json:"requested"`
							} `json:"summary"`
						}
						if err := json.Unmarshal(out.Bytes(), &data); err != nil {
							t.Fatal(err)
						}
						wantRows := min(test.want, 6)
						if empty {
							wantRows = 0
						}
						if data.Summary.Requested != test.want || len(data.Items) != wantRows {
							t.Fatalf("requested=%d rows=%d want requested=%d rows=%d", data.Summary.Requested, len(data.Items), test.want, wantRows)
						}
					})
				}
			}
		})
	}
}
