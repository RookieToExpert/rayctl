package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"rayctl/internal/platform"
)

type renewFake struct {
	refs      []platform.VCKubeconfigRef
	deleted   bool
	err       error
	downloads int
}

func (f *renewFake) VCKubeconfigInventory(context.Context, string) ([]platform.VCKubeconfigRef, []error) {
	return f.refs, nil
}
func (f *renewFake) DownloadVCKubeconfig(context.Context, platform.VCKubeconfigRef) ([]byte, error) {
	f.downloads++
	return nil, errors.New("unexpected download")
}
func (f *renewFake) VCKubeconfigDeleted(context.Context, platform.VCKubeconfigRef) (bool, error) {
	return f.deleted, f.err
}
func TestRenewPreservesExisting(t *testing.T) {
	root := t.TempDir()
	ref := platform.VCKubeconfigRef{Name: "vc-demo", Environment: "d"}
	path, _ := renewPath(root, ref)
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte("original"), 0600)
	f := &renewFake{refs: []platform.VCKubeconfigRef{ref}}
	if err := RenewVCKubeconfigs(context.Background(), f, VCRenewOptions{Root: root, Environment: "d", Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "original" || f.downloads != 0 {
		t.Fatal("existing config was touched")
	}
}
func TestRenewDeletionRequiresEvidenceAndConsent(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		deleted, consent, dry, modified bool
		err                             error
		remove                          bool
	}{
		{name: "403", err: errors.New("403")}, {name: "404", err: errors.New("404")}, {name: "missing"},
		{name: "declined", deleted: true}, {name: "confirmed", deleted: true, consent: true, remove: true},
		{name: "dry", deleted: true, consent: true, dry: true}, {name: "modified", deleted: true, consent: true, modified: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			ref := platform.VCKubeconfigRef{Name: "vc-old", UID: "uid", Environment: "d"}
			path, _ := renewPath(root, ref)
			os.MkdirAll(filepath.Dir(path), 0700)
			os.WriteFile(path, []byte("original"), 0600)
			manifest, _ := json.Marshal(map[string]renewEntry{path: {Ref: ref, Hash: fmtHash([]byte("original"))}})
			os.WriteFile(filepath.Join(root, ".rayctl-vc-renew.json"), manifest, 0600)
			if tc.modified {
				os.WriteFile(path, []byte("changed"), 0600)
			}
			err := RenewVCKubeconfigs(context.Background(), &renewFake{deleted: tc.deleted, err: tc.err}, VCRenewOptions{Root: root, Environment: "d", DryRun: tc.dry, Confirm: func([]string) bool { return tc.consent }})
			if err != nil {
				t.Fatal(err)
			}
			_, err = os.Stat(path)
			if os.IsNotExist(err) != tc.remove {
				t.Fatalf("unexpected deletion: %v", err)
			}
		})
	}
}
func fmtHash(data []byte) string {
	const digits = "0123456789abcdef"
	h := sha256.Sum256(data)
	out := make([]byte, 64)
	for i, b := range h {
		out[2*i] = digits[b>>4]
		out[2*i+1] = digits[b&15]
	}
	return string(out)
}
func TestRenewDryRunDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	f := &renewFake{refs: []platform.VCKubeconfigRef{{Name: "vc-new", Environment: "d"}}}
	if err := RenewVCKubeconfigs(context.Background(), f, VCRenewOptions{Root: root, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 || f.downloads != 0 {
		t.Fatal("dry run modified filesystem or fetched credentials")
	}
}
func TestRenewAtomicCreateDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "D", "vc-test")
	if err := createRenewFile(path, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := createRenewFile(path, []byte("two")); err == nil {
		t.Fatal("overwrote file")
	}
	data, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	if string(data) != "one" || info.Mode().Perm() != 0600 {
		t.Fatal("content or mode changed")
	}
}
