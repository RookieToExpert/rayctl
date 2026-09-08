package podevidence

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type imageLoad struct {
	done  chan struct{}
	image Image
	err   error
}

type imageCache struct {
	mu    sync.Mutex
	loads map[string]*imageLoad
}

func (c *imageCache) lookup(ctx context.Context, client kubernetes.Interface, pod corev1.Pod, image Image) (Image, error) {
	key := pod.Namespace + "|" + image.Ref + "|" + image.Digest
	for _, secret := range pod.Spec.ImagePullSecrets {
		key += "|" + secret.Name
	}
	c.mu.Lock()
	if load, ok := c.loads[key]; ok {
		c.mu.Unlock()
		select {
		case <-load.done:
			return load.image, load.err
		case <-ctx.Done():
			return image, ctx.Err()
		}
	}
	load := &imageLoad{done: make(chan struct{})}
	c.loads[key] = load
	c.mu.Unlock()
	load.image, load.err = lookupImage(ctx, client, pod, image)
	close(load.done)
	return load.image, load.err
}

func artifactURL(ref string, imageID string) (string, string, error) {
	parts := strings.SplitN(ref, "/", 3)
	if len(parts) != 3 || !strings.ContainsAny(parts[0], ".:") {
		return "", "", fmt.Errorf("image is not an explicit Harbor project/repository reference")
	}
	registry, project, repository := parts[0], parts[1], parts[2]
	reference := "latest"
	if at := strings.LastIndex(repository, "@"); at >= 0 {
		reference, repository = repository[at+1:], repository[:at]
	} else if colon := strings.LastIndex(repository, ":"); colon > strings.LastIndex(repository, "/") {
		reference, repository = repository[colon+1:], repository[:colon]
	}
	// Prefer the actual running image digest over a mutable tag.
	if at := strings.LastIndex(imageID, "@sha256:"); at >= 0 {
		reference = imageID[at+1:]
	}
	return "https://" + registry + "/api/v2.0/projects/" + url.PathEscape(project) + "/repositories/" + url.PathEscape(url.PathEscape(repository)) + "/artifacts/" + url.PathEscape(reference) + "?with_tag=true", registry, nil
}

func lookupImage(ctx context.Context, client kubernetes.Interface, pod corev1.Pod, image Image) (Image, error) {
	endpoint, registry, err := artifactURL(image.Ref, image.Digest)
	if err != nil {
		return image, err
	}
	cachePath := registryFailurePath(registry)
	if info, err := os.Stat(cachePath); err == nil && time.Since(info.ModTime()) >= 0 && time.Since(info.ModTime()) < time.Minute {
		return image, fmt.Errorf("Harbor %s recently unreachable; skipping for up to 60s (or use --no-image)", registry)
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	username, password := "", ""
	for _, reference := range pod.Spec.ImagePullSecrets {
		secret, err := client.CoreV1().Secrets(pod.Namespace).Get(ctx, reference.Name, metav1.GetOptions{})
		if err != nil {
			continue
		}
		var config struct {
			Auths map[string]struct {
				Username string
				Password string
				Auth     string
			}
		}
		if json.Unmarshal(secret.Data[corev1.DockerConfigJsonKey], &config) != nil {
			continue
		}
		for host, auth := range config.Auths {
			host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
			host = strings.TrimSuffix(strings.TrimSuffix(host, "/v1/"), "/")
			if host != registry {
				continue
			}
			username, password = auth.Username, auth.Password
			if username == "" && auth.Auth != "" {
				if decoded, err := base64.StdEncoding.DecodeString(auth.Auth); err == nil {
					username, password, _ = strings.Cut(string(decoded), ":")
				}
			}
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return image, err
	}
	if username != "" {
		request.SetBasicAuth(username, password)
	}
	httpClient := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := httpClient.Do(request)
	if err != nil {
		// Only cache transport failures, never artifact/authentication failures.
		if ctx.Err() == nil || ctx.Err() == context.DeadlineExceeded {
			rememberRegistryFailure(cachePath)
		}
		return image, fmt.Errorf("Harbor request failed: %w", err)
	}
	defer response.Body.Close()
	if response.Header.Get("X-Squid-Error") != "" {
		rememberRegistryFailure(cachePath)
		return image, fmt.Errorf("Harbor request blocked by proxy (HTTP %d)", response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		return image, fmt.Errorf("Harbor returned HTTP %d", response.StatusCode)
	}
	var artifact struct {
		Extra struct {
			Architecture string `json:"architecture"`
			OS           string `json:"os"`
		} `json:"extra_attrs"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1024*1024)).Decode(&artifact); err != nil {
		return image, fmt.Errorf("invalid Harbor response: %w", err)
	}
	if artifact.Extra.Architecture != "" {
		image.Architecture = &artifact.Extra.Architecture
	}
	if artifact.Extra.OS != "" {
		image.OS = &artifact.Extra.OS
	}
	if image.Architecture == nil {
		return image, fmt.Errorf("artifact has no single architecture (may be a multi-platform index)")
	}
	return image, nil
}

func registryFailurePath(registry string) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	// Hash proxy configuration too: switching networks must not reuse a stale failure.
	key := registry
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "NO_PROXY", "no_proxy"} {
		key += "|" + name + "=" + os.Getenv(name)
	}
	return filepath.Join(dir, "rayctl", "registry-failures", fmt.Sprintf("%x", sha256.Sum256([]byte(key))))
}

func rememberRegistryFailure(path string) {
	if path == "" {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0700) == nil {
		_ = os.WriteFile(path, nil, 0600)
	}
}
