package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHCTargetsRespectEnvironmentAndExplicitConfig(t *testing.T) {
	dir := t.TempDir()
	paths := map[string]string{"d": filepath.Join(dir, "kubeconfig"), "pt": filepath.Join(dir, "kubeconfigpt")}
	for env, path := range paths {
		data := "apiVersion: v1\nkind: Config\nclusters:\n- name: test\n  cluster:\n    server: https://" + env + ".example.test\ncontexts:\n- name: test\n  context:\n    cluster: test\n    user: test\ncurrent-context: test\nusers:\n- name: test\n  user: {}\n"
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("RAYCTL_HC_KUBECONFIG_"+map[string]string{"d": "D", "pt": "PT"}[env], path)
	}
	all, err := resolveHCTargets("all", paths["d"], true)
	if err != nil || len(all) != 2 || all[0].Name != "d" || all[1].Name != "pt" || all[0].Err != nil || all[1].Err != nil {
		t.Fatalf("%#v %v", all, err)
	}
	pt, err := resolveHCTargets("pt", paths["d"], false)
	if err != nil || len(pt) != 1 || pt[0].Kubeconfig != paths["pt"] {
		t.Fatalf("%#v %v", pt, err)
	}
	if _, err := resolveHCTargets("pt", paths["d"], true); err == nil {
		t.Fatal("explicit conflicting -k was ignored")
	}
}
