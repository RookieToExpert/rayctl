package kube

import (
	"path/filepath"
	"testing"
)

func TestResolveKubeconfigPathUsesCurrentUserHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KUBECONFIG", "")

	want := filepath.Join(home, "kubeconfig")
	if got := resolveKubeconfigPath(""); got != want {
		t.Fatalf("resolveKubeconfigPath(\"\") = %q, want %q", got, want)
	}
}

func TestResolveKubeconfigPathPrecedence(t *testing.T) {
	t.Setenv("KUBECONFIG", "/env/kubeconfig")
	if got := resolveKubeconfigPath(""); got != "/env/kubeconfig" {
		t.Fatalf("environment path = %q, want /env/kubeconfig", got)
	}
	if got := resolveKubeconfigPath("/flag/kubeconfig"); got != "/flag/kubeconfig" {
		t.Fatalf("flag path = %q, want /flag/kubeconfig", got)
	}
}
