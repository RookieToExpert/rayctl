package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rayctl/internal/platform"
)

type renewFake struct {
	refs            []platform.VCKubeconfigRef
	inventoryErrors []error
	downloadError   error
	downloads       int
}

func (f *renewFake) VCKubeconfigInventory(context.Context, string) ([]platform.VCKubeconfigRef, []error) {
	return f.refs, f.inventoryErrors
}
func (f *renewFake) DownloadVCKubeconfig(context.Context, platform.VCKubeconfigRef) ([]byte, error) {
	f.downloads++
	if f.downloadError != nil {
		return nil, f.downloadError
	}
	return nil, errors.New("unexpected download")
}

func writeRenewTestConfig(t *testing.T, root, env, name, content string) (platform.VCKubeconfigRef, string) {
	t.Helper()
	ref := platform.VCKubeconfigRef{Name: name, UID: name + "-uid", Environment: env}
	path, err := renewPath(root, ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return ref, path
}

func writeRenewTestManifest(t *testing.T, root string, entries map[string]renewEntry) []byte {
	t.Helper()
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".rayctl-vc-renew.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return data
}

func requireRenewTestContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s content = %q, want %q", path, data, want)
	}
}

func TestRenewPreservesExisting(t *testing.T) {
	root := t.TempDir()
	ref, path := writeRenewTestConfig(t, root, "d", "vc-demo", "original")
	f := &renewFake{refs: []platform.VCKubeconfigRef{ref}}
	if err := RenewVCKubeconfigs(context.Background(), f, VCRenewOptions{Root: root, Environment: "d", Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	requireRenewTestContent(t, path, "original")
	if f.downloads != 0 {
		t.Fatal("existing config was fetched again")
	}
}

func TestRenewRemovesAbsentFilesWithConsent(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		tracked, modified, dry       bool
		consent, changeDuringConfirm bool
		remove                       bool
	}{
		{name: "tracked", tracked: true, consent: true, remove: true},
		{name: "legacy", consent: true, remove: true},
		{name: "tracked with prior local edits", tracked: true, modified: true, consent: true, remove: true},
		{name: "declined", tracked: true},
		{name: "legacy declined"},
		{name: "dry run", tracked: true, dry: true, consent: true},
		{name: "legacy dry run", dry: true, consent: true},
		{name: "changed while confirming", tracked: true, consent: true, changeDuringConfirm: true},
		{name: "legacy changed while confirming", consent: true, changeDuringConfirm: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			ref, path := writeRenewTestConfig(t, root, "d", "vc-old", "original")
			var originalManifest []byte
			if tc.tracked {
				originalManifest = writeRenewTestManifest(t, root, map[string]renewEntry{path: {Ref: ref, Hash: fmtHash([]byte("original"))}})
			}
			content := "original"
			if tc.modified {
				content = "edited before renew"
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var output bytes.Buffer
			confirmCalls := 0
			err := RenewVCKubeconfigs(context.Background(), &renewFake{}, VCRenewOptions{
				Root: root, Environment: "d", DryRun: tc.dry, Out: &output,
				Confirm: func(paths []string) bool {
					confirmCalls++
					if len(paths) != 1 || paths[0] != path {
						t.Fatalf("removal candidates = %v, want [%s]", paths, path)
					}
					if tc.changeDuringConfirm {
						content = "edited while confirming"
						if err := os.WriteFile(path, []byte(content), 0600); err != nil {
							t.Fatal(err)
						}
					}
					return tc.consent
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = os.Lstat(path)
			if os.IsNotExist(err) != tc.remove {
				t.Fatalf("unexpected deletion: %v; output: %s", err, output.String())
			}
			if !tc.remove {
				requireRenewTestContent(t, path, content)
			}
			wantConfirmCalls := 1
			if tc.dry {
				wantConfirmCalls = 0
			}
			if confirmCalls != wantConfirmCalls {
				t.Fatalf("confirmation calls = %d, want %d", confirmCalls, wantConfirmCalls)
			}
			if !strings.Contains(output.String(), "REMOVE CANDIDATE "+path+" (VC absent from platform inventory)") {
				t.Fatalf("missing candidate explanation: %s", output.String())
			}
			wantSummary := "added=0 kept=1 removed=0 failed=0"
			if tc.remove {
				wantSummary = "added=0 kept=0 removed=1 failed=0"
				if !strings.Contains(output.String(), "REMOVED "+path) {
					t.Fatalf("missing removal output: %s", output.String())
				}
			}
			if !strings.Contains(output.String(), wantSummary) {
				t.Fatalf("missing summary %q: %s", wantSummary, output.String())
			}
			manifestPath := filepath.Join(root, ".rayctl-vc-renew.json")
			if tc.dry {
				if tc.tracked {
					data, err := os.ReadFile(manifestPath)
					if err != nil || !bytes.Equal(data, originalManifest) {
						t.Fatalf("dry run changed manifest: %v", err)
					}
				} else if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
					t.Fatalf("dry run wrote manifest: %v", err)
				}
			} else if tc.tracked {
				data, err := os.ReadFile(manifestPath)
				if err != nil {
					t.Fatal(err)
				}
				entries := map[string]renewEntry{}
				if err := json.Unmarshal(data, &entries); err != nil {
					t.Fatal(err)
				}
				_, present := entries[path]
				if present == tc.remove {
					t.Fatalf("manifest tracking present = %v, removed = %v", present, tc.remove)
				}
			}
		})
	}
}

func TestRenewInventoryFailurePreservesAllMissingFiles(t *testing.T) {
	root := t.TempDir()
	present, presentPath := writeRenewTestConfig(t, root, "d", "vc-present", "present")
	tracked, trackedPath := writeRenewTestConfig(t, root, "d", "vc-tracked", "tracked")
	_, legacyPath := writeRenewTestConfig(t, root, "d", "vc-legacy", "legacy")
	writeRenewTestManifest(t, root, map[string]renewEntry{trackedPath: {Ref: tracked, Hash: fmtHash([]byte("tracked"))}})
	inventoryErr := errors.New("inventory request failed")
	f := &renewFake{refs: []platform.VCKubeconfigRef{present}, inventoryErrors: []error{inventoryErr}}
	var output bytes.Buffer
	err := RenewVCKubeconfigs(context.Background(), f, VCRenewOptions{
		Root: root, Environment: "d", Out: &output,
		Confirm: func([]string) bool {
			t.Fatal("incomplete inventory must not ask to delete files")
			return true
		},
	})
	if !errors.Is(err, inventoryErr) {
		t.Fatalf("error = %v, want inventory failure", err)
	}
	requireRenewTestContent(t, presentPath, "present")
	requireRenewTestContent(t, trackedPath, "tracked")
	requireRenewTestContent(t, legacyPath, "legacy")
	if f.downloads != 0 || strings.Contains(output.String(), "REMOVE CANDIDATE") {
		t.Fatalf("incomplete inventory caused fetch or removal: downloads=%d, output=%s", f.downloads, output.String())
	}
	if !strings.Contains(output.String(), "added=0 kept=3 removed=0 failed=1") {
		t.Fatalf("missing kept count for legacy/tracked files: %s", output.String())
	}
}

func TestRenewDownloadFailureDoesNotBlockRemoval(t *testing.T) {
	root := t.TempDir()
	_, path := writeRenewTestConfig(t, root, "d", "vc-gone", "legacy")
	downloadErr := errors.New("no complete certificate credential available")
	f := &renewFake{refs: []platform.VCKubeconfigRef{{Name: "vc-new", Environment: "d"}}, downloadError: downloadErr}
	var output bytes.Buffer
	err := RenewVCKubeconfigs(context.Background(), f, VCRenewOptions{
		Root: root, Environment: "d", Out: &output,
		Confirm: func(paths []string) bool {
			if len(paths) != 1 || paths[0] != path {
				t.Fatalf("removal candidates = %v, want [%s]", paths, path)
			}
			return true
		},
	})
	if !errors.Is(err, downloadErr) {
		t.Fatalf("error = %v, want download failure", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("absent VC was retained despite complete inventory: %v", err)
	}
	if f.downloads != 1 || !strings.Contains(output.String(), "added=0 kept=0 removed=1 failed=1") {
		t.Fatalf("unexpected result: downloads=%d, output=%s", f.downloads, output.String())
	}
}

func TestRenewOnlyRemovesSelectedEnvironment(t *testing.T) {
	root := t.TempDir()
	_, selectedPath := writeRenewTestConfig(t, root, "d", "vc-gone", "selected")
	ptRef, ptPath := writeRenewTestConfig(t, root, "pt", "vc-gone", "pt")
	_, cloudPath := writeRenewTestConfig(t, root, "dcloud", "vc-gone", "dcloud")
	writeRenewTestManifest(t, root, map[string]renewEntry{ptPath: {Ref: ptRef, Hash: fmtHash([]byte("pt"))}})
	if err := RenewVCKubeconfigs(context.Background(), &renewFake{}, VCRenewOptions{
		Root: root, Environment: "d",
		Confirm: func(paths []string) bool {
			if len(paths) != 1 || paths[0] != selectedPath {
				t.Fatalf("removal candidates = %v, want only selected environment", paths)
			}
			return true
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(selectedPath); !os.IsNotExist(err) {
		t.Fatalf("selected absent VC was retained: %v", err)
	}
	requireRenewTestContent(t, ptPath, "pt")
	requireRenewTestContent(t, cloudPath, "dcloud")
}

func TestRenewPreservesSymlinksDirectoriesAndOtherFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "D")
	if err := os.MkdirAll(filepath.Join(dir, "vc-directory"), 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("target"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "vc-symlink")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "notes")
	if err := os.WriteFile(other, []byte("notes"), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RenewVCKubeconfigs(context.Background(), &renewFake{}, VCRenewOptions{
		Root: root, Environment: "d", Out: &output,
		Confirm: func([]string) bool {
			t.Fatal("symlinks and directories must not become deletion candidates")
			return true
		},
	}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink was changed: %v", err)
	}
	if info, err := os.Stat(filepath.Join(dir, "vc-directory")); err != nil || !info.IsDir() {
		t.Fatalf("directory was changed: %v", err)
	}
	requireRenewTestContent(t, target, "target")
	requireRenewTestContent(t, other, "notes")
	if !strings.Contains(output.String(), "added=0 kept=2 removed=0 failed=0") {
		t.Fatalf("missing preserved VC file count: %s", output.String())
	}
}

func TestRenewPreservesUnreadableAbsentFile(t *testing.T) {
	root := t.TempDir()
	_, path := writeRenewTestConfig(t, root, "d", "vc-unreadable", "original")
	if err := os.Chmod(path, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0600) })
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("current user can read files without read permission")
	}
	var output bytes.Buffer
	err := RenewVCKubeconfigs(context.Background(), &renewFake{}, VCRenewOptions{
		Root: root, Environment: "d", Out: &output,
		Confirm: func([]string) bool {
			t.Fatal("unreadable files must not become deletion candidates")
			return true
		},
	})
	if err == nil {
		t.Fatal("unreadable file should report a failure")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("unreadable file was removed: %v", err)
	}
	if !strings.Contains(output.String(), "added=0 kept=1 removed=0 failed=1") {
		t.Fatalf("missing preserved unreadable file count: %s", output.String())
	}
}

func TestRenewPreservesFileReplacedBySymlinkWhileConfirming(t *testing.T) {
	root := t.TempDir()
	_, path := writeRenewTestConfig(t, root, "d", "vc-old", "original")
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RenewVCKubeconfigs(context.Background(), &renewFake{}, VCRenewOptions{
		Root: root, Environment: "d",
		Confirm: func([]string) bool {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			return true
		},
	}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("replacement symlink was removed: %v", err)
	}
	requireRenewTestContent(t, target, "original")
}

func TestRenewPreservesFilesWhenDirectoryBecomesSymlinkWhileConfirming(t *testing.T) {
	root := t.TempDir()
	_, path := writeRenewTestConfig(t, root, "d", "vc-old", "original")
	dir := filepath.Dir(path)
	savedDir := filepath.Join(root, "D-original")
	targetDir := filepath.Join(root, "replacement")
	if err := os.Mkdir(targetDir, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(targetDir, "vc-old")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RenewVCKubeconfigs(context.Background(), &renewFake{}, VCRenewOptions{
		Root: root, Environment: "d", Out: &output,
		Confirm: func([]string) bool {
			if err := os.Rename(dir, savedDir); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(targetDir, dir); err != nil {
				t.Fatal(err)
			}
			return true
		},
	}); err != nil {
		t.Fatal(err)
	}
	requireRenewTestContent(t, filepath.Join(savedDir, "vc-old"), "original")
	requireRenewTestContent(t, target, "original")
	if !strings.Contains(output.String(), "added=0 kept=1 removed=0 failed=0") {
		t.Fatalf("replacement directory was not reported as preserved: %s", output.String())
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
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
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
	requireRenewTestContent(t, path, "one")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("file mode changed")
	}
}

func TestRenewCancellationPreservesRemovalCandidates(t *testing.T) {
	for _, beforeStart := range []bool{false, true} {
		t.Run(map[bool]string{true: "before start", false: "during confirmation"}[beforeStart], func(t *testing.T) {
			root := t.TempDir()
			_, path := writeRenewTestConfig(t, root, "d", "vc-old", "keep this config")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if beforeStart {
				cancel()
			}
			err := RenewVCKubeconfigs(ctx, &renewFake{}, VCRenewOptions{Root: root, Environment: "d", Out: io.Discard, Confirm: func([]string) bool { cancel(); return true }})
			if !errors.Is(err, context.Canceled) {
				t.Errorf("cancellation lost: %v", err)
			}
			requireRenewTestContent(t, path, "keep this config")
		})
	}
}
