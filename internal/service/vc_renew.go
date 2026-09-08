package service

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"rayctl/internal/platform"
)

type VCRenewAPI interface {
	VCKubeconfigInventory(context.Context, string) ([]platform.VCKubeconfigRef, []error)
	DownloadVCKubeconfig(context.Context, platform.VCKubeconfigRef) ([]byte, error)
	VCKubeconfigDeleted(context.Context, platform.VCKubeconfigRef) (bool, error)
}
type renewEntry struct {
	Ref  platform.VCKubeconfigRef `json:"resource"`
	Hash string                   `json:"sha256"`
}
type VCRenewOptions struct {
	Root, Environment        string
	DryRun, SkipConnectivity bool
	Out                      io.Writer
	Confirm                  func([]string) bool
}

func renewPath(root string, ref platform.VCKubeconfigRef) (string, error) {
	dirs := map[string]string{"d": "D", "pt": "PT", "dcloud": "Dcloud"}
	dir, ok := dirs[ref.Environment]
	if !ok || !strings.HasPrefix(ref.Name, "vc-") || strings.ContainsAny(ref.Name, "/\\\x00") || filepath.Base(ref.Name) != ref.Name {
		return "", fmt.Errorf("unsafe VC name/environment")
	}
	return filepath.Join(root, dir, ref.Name), nil
}

func validateRenewConfig(data []byte, connect bool) error {
	config, err := clientcmd.RESTConfigFromKubeConfig(data)
	if err != nil {
		return fmt.Errorf("invalid kubeconfig")
	}
	pair, err := tls.X509KeyPair(config.CertData, config.KeyData)
	if err != nil {
		return fmt.Errorf("invalid certificate/key pair")
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return fmt.Errorf("invalid client certificate")
	}
	if time.Now().Before(cert.NotBefore) || time.Now().After(cert.NotAfter) {
		return fmt.Errorf("client certificate is not currently valid")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(config.CAData) {
		return fmt.Errorf("invalid certificate authority")
	}
	if !connect {
		return nil
	}
	config.Timeout = 5 * time.Second
	client, err := rest.HTTPClientFor(config)
	if err != nil {
		return fmt.Errorf("invalid TLS configuration")
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Get(strings.TrimRight(config.Host, "/") + "/version")
	if err != nil {
		return fmt.Errorf("VC connectivity verification failed")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("VC connectivity returned HTTP %d", response.StatusCode)
	}
	return nil
}

// Link publishes a fully-written config without ever replacing an existing path.
func createRenewFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("unsafe output directory")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".vc-renew-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Link(f.Name(), path)
}

func RenewVCKubeconfigs(ctx context.Context, api VCRenewAPI, options VCRenewOptions) error {
	if options.Out == nil {
		options.Out = io.Discard
	}
	root, err := filepath.Abs(options.Root)
	if err != nil {
		return err
	}
	manifest := filepath.Join(root, ".rayctl-vc-renew.json")
	entries := map[string]renewEntry{}
	if !options.DryRun {
		lock, err := os.OpenFile(manifest+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("renew lock unavailable: %w", err)
		}
		lock.Close()
		defer os.Remove(manifest + ".lock")
	}
	data, err := os.ReadFile(manifest)
	if err == nil {
		if err = json.Unmarshal(data, &entries); err != nil {
			return fmt.Errorf("invalid renew manifest; refusing changes")
		}
		if entries == nil {
			return fmt.Errorf("invalid null renew manifest; refusing changes")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	refs, failures := api.VCKubeconfigInventory(ctx, options.Environment)
	for _, err := range failures {
		fmt.Fprintf(options.Out, "WARNING %v; affected files are preserved\n", err)
	}
	paths := map[string][]platform.VCKubeconfigRef{}
	for _, ref := range refs {
		path, err := renewPath(root, ref)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		paths[path] = append(paths[path], ref)
	}
	keys := make([]string, 0, len(paths))
	for env, dir := range map[string]string{"d": "D", "pt": "PT", "dcloud": "Dcloud"} {
		if options.Environment != "all" && options.Environment != "" && options.Environment != "auto" && options.Environment != env {
			continue
		}
		files, readErr := os.ReadDir(filepath.Join(root, dir))
		if readErr != nil && !os.IsNotExist(readErr) {
			failures = append(failures, readErr)
		}
		for _, file := range files {
			path := filepath.Join(root, dir, file.Name())
			if strings.HasPrefix(file.Name(), "vc-") && len(paths[path]) == 0 {
				if _, tracked := entries[path]; !tracked {
					fmt.Fprintf(options.Out, "KEEP %s (legacy/unmanaged; deletion not confirmed)\n", path)
				}
			}
		}
	}
	for path := range paths {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	added, kept := 0, 0
	for _, path := range keys {
		if _, err := os.Lstat(path); err == nil {
			fmt.Fprintf(options.Out, "KEEP %s\n", path)
			kept++
			continue
		} else if !os.IsNotExist(err) {
			failures = append(failures, err)
			continue
		}
		refs := paths[path]
		if len(refs) != 1 {
			failures = append(failures, fmt.Errorf("ambiguous VC file %s; skipped", path))
			continue
		}
		if options.DryRun {
			fmt.Fprintf(options.Out, "WOULD ADD %s (%s)\n", path, refs[0].Profile)
			continue
		}
		query, cancel := context.WithTimeout(ctx, 20*time.Second)
		config, err := api.DownloadVCKubeconfig(query, refs[0])
		cancel()
		if err == nil {
			err = validateRenewConfig(config, !options.SkipConnectivity)
		}
		if err == nil {
			err = createRenewFile(path, config)
		}
		if err != nil {
			fmt.Fprintf(options.Out, "FAILED %s: %v\n", path, err)
			failures = append(failures, err)
			continue
		}
		entries[path] = renewEntry{Ref: refs[0], Hash: fmt.Sprintf("%x", sha256.Sum256(config))}
		added++
		fmt.Fprintf(options.Out, "ADDED %s\n", path)
	}
	removals := []string{}
	for path, entry := range entries {
		expected, err := renewPath(root, entry.Ref)
		if err != nil || expected != path {
			continue
		}
		if options.Environment != "" && options.Environment != "auto" && options.Environment != "all" && entry.Ref.Environment != options.Environment {
			continue
		}
		if len(paths[path]) > 0 {
			continue
		}
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(content)) != entry.Hash {
			fmt.Fprintf(options.Out, "KEEP %s (locally modified)\n", path)
			continue
		}
		query, cancel := context.WithTimeout(ctx, 5*time.Second)
		deleted, err := api.VCKubeconfigDeleted(query, entry.Ref)
		cancel()
		if err != nil || !deleted {
			fmt.Fprintf(options.Out, "KEEP %s (deletion not confirmed)\n", path)
			continue
		}
		removals = append(removals, path)
	}
	sort.Strings(removals)
	for _, path := range removals {
		fmt.Fprintf(options.Out, "REMOVE CANDIDATE %s\n", path)
	}
	removed := 0
	if !options.DryRun && len(removals) > 0 && options.Confirm != nil && options.Confirm(removals) {
		for _, path := range removals {
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			content, err := os.ReadFile(path)
			if err != nil || fmt.Sprintf("%x", sha256.Sum256(content)) != entries[path].Hash {
				continue
			}
			if err = os.Remove(path); err != nil {
				failures = append(failures, err)
			} else {
				delete(entries, path)
				removed++
				fmt.Fprintf(options.Out, "REMOVED %s\n", path)
			}
		}
	}
	if !options.DryRun {
		bytes, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return err
		}
		f, err := os.CreateTemp(root, ".vc-manifest-*")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if _, err = f.Write(bytes); err != nil {
			f.Close()
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
		if err = os.Rename(f.Name(), manifest); err != nil {
			return err
		}
	}
	fmt.Fprintf(options.Out, "added=%d kept=%d removed=%d failed=%d\n", added, kept, removed, len(failures))
	return errors.Join(failures...)
}
