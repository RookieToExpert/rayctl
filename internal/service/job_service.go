package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"net"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

var (
	volcanoJobGVR = schema.GroupVersionResource{
		Group:    "batch.volcano.sh",
		Version:  "v1alpha1",
		Resource: "jobs",
	}
	volcanoPodGroupGVR = schema.GroupVersionResource{
		Group:    "scheduling.volcano.sh",
		Version:  "v1beta1",
		Resource: "podgroups",
	}
	utcPlus8 = time.FixedZone("UTC+8", 8*60*60)
)

const (
	defaultTailLogLines         = int64(10)
	defaultEventLimit           = 10
	defaultRegistryHost         = "registry2.d.pjlab.org.cn"
	jobUIDLookupPageSize        = int64(5000)
	platformJobScanTimeout      = 4 * time.Second
	platformJobProbeConcurrency = 16
	registryLookupTimeout       = 750 * time.Millisecond
	dockerLoginTimeout          = 3 * time.Second
)

func looksLikeUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, ch := range value {
		switch i {
		case 8, 13, 18, 23:
			if ch != '-' {
				return false
			}
		default:
			if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') && (ch < 'A' || ch > 'F') {
				return false
			}
		}
	}
	return true
}

func (s *JobService) resolveImagePullSecretsFromPlatform(ctx context.Context, identity *jobIdentity, namespace string, secretNames []string) []string {
	if s.vcClient == nil || identity == nil || strings.TrimSpace(identity.VClusterName) == "" || strings.TrimSpace(namespace) == "" {
		return secretNames
	}

	resolved := make([]string, 0, len(secretNames))
	for _, secretName := range secretNames {
		secret, err := s.getPlatformSecret(ctx, identity, namespace, secretName)
		if err != nil {
			resolved = append(resolved, secretName)
			continue
		}
		resolved = append(resolved, formatImagePullSecret(secretName, secret))
	}
	return resolved
}

func (s *JobService) resolveImagePullSecrets(ctx context.Context, identity *jobIdentity, secretNames []string) []string {
	hostNamespace := ""
	vclusterName := ""
	namespace := ""
	if identity != nil {
		hostNamespace = strings.TrimSpace(identity.HostNamespace)
		vclusterName = strings.TrimSpace(identity.VClusterName)
		namespace = strings.TrimSpace(identity.Namespace)
	}

	if hostNamespace != "" {
		return s.resolveImagePullSecretsFromKube(ctx, hostNamespace, secretNames)
	}
	if vclusterName != "" && namespace != "" {
		return s.resolveImagePullSecretsFromPlatform(ctx, identity, namespace, secretNames)
	}
	return secretNames
}

func (s *JobService) resolveImagePullSecretsFromKube(ctx context.Context, namespace string, secretNames []string) []string {
	if strings.TrimSpace(namespace) == "" {
		return secretNames
	}

	return boundedMap(ctx, secretNames, 4, func(ctx context.Context, secretName string) string {
		secret, err := s.clientset.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			return secretName
		}
		return formatImagePullSecret(secretName, secret)
	})
}

func formatImagePullSecret(secretName string, secret *corev1.Secret) string {
	creds := decodeDockerSecret(secret)
	if len(creds) == 0 {
		return secretName
	}
	return fmt.Sprintf("%s | %s", secretName, strings.Join(creds, "; "))
}

func decodeDockerSecret(secret *corev1.Secret) []string {
	credentials := decodeDockerCredentials(secret)
	results := make([]string, 0, len(credentials))
	for _, credential := range credentials {
		registryDisplay := credential.Registry
		if credential.RegistryHasWhitespace {
			registryDisplay = fmt.Sprintf("%q [registry 两端有空格]", credential.RegistryRaw)
		}
		if credential.Registry != "" {
			results = append(results, fmt.Sprintf("%s username: %s password: %s", registryDisplay, credential.Username, credential.Password))
		} else {
			results = append(results, fmt.Sprintf("username: %s password: %s", credential.Username, credential.Password))
		}
	}
	return results
}

func decodeDockerCredentials(secret *corev1.Secret) []dockerCredential {
	if secret == nil {
		return nil
	}

	type dockerConfigEntry struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Auth     string `json:"auth"`
	}
	type dockerConfigJSON struct {
		Auths map[string]dockerConfigEntry `json:"auths"`
	}

	var raw []byte
	switch {
	case len(secret.Data[corev1.DockerConfigJsonKey]) > 0:
		raw = secret.Data[corev1.DockerConfigJsonKey]
	case len(secret.Data[corev1.DockerConfigKey]) > 0:
		raw = secret.Data[corev1.DockerConfigKey]
	default:
		return nil
	}

	var payload dockerConfigJSON
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}

	registries := make([]string, 0, len(payload.Auths))
	for registry := range payload.Auths {
		registries = append(registries, registry)
	}
	sort.Strings(registries)

	results := make([]dockerCredential, 0, len(registries))
	for _, registry := range registries {
		entry := payload.Auths[registry]
		registryTrimmed := strings.TrimSpace(registry)
		username := strings.TrimSpace(entry.Username)
		password := strings.TrimSpace(entry.Password)
		if (username == "" || password == "") && strings.TrimSpace(entry.Auth) != "" {
			decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
			if err == nil {
				parts := strings.SplitN(string(decoded), ":", 2)
				if len(parts) == 2 {
					if username == "" {
						username = parts[0]
					}
					if password == "" {
						password = parts[1]
					}
				}
			}
		}
		if username == "" && password == "" {
			continue
		}
		results = append(results, dockerCredential{
			Registry:              registryTrimmed,
			RegistryRaw:           registry,
			RegistryHasWhitespace: registryTrimmed != registry,
			Username:              username,
			Password:              password,
		})
	}

	return results
}

func joinMessageReason(message string, reason string) string {
	message = strings.TrimSpace(message)
	reason = strings.TrimSpace(reason)
	switch {
	case message == "" && reason == "":
		return ""
	case message == "":
		return reason
	case reason == "", strings.EqualFold(message, reason):
		return message
	default:
		return fmt.Sprintf("%s | %s", message, reason)
	}
}

func getNestedString(obj map[string]any, fields ...string) string {
	value, found, err := unstructured.NestedString(obj, fields...)
	if err != nil || !found {
		return ""
	}
	return strings.TrimSpace(value)
}

func getNestedText(obj map[string]any, fields ...string) string {
	if value := getNestedString(obj, fields...); value != "" {
		return value
	}
	raw, found, err := unstructured.NestedFieldNoCopy(obj, fields...)
	if err != nil || !found || raw == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", raw))
}

func eventTimestamp(event corev1.Event) time.Time {
	switch {
	case !event.EventTime.IsZero():
		return event.EventTime.Time
	case !event.LastTimestamp.IsZero():
		return event.LastTimestamp.Time
	case !event.FirstTimestamp.IsZero():
		return event.FirstTimestamp.Time
	default:
		return event.CreationTimestamp.Time
	}
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func dashIfEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func boolToYesNo(value bool) string {
	if value {
		return "Yes"
	}
	return "No"
}

func extractResourceUIDFromName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if looksLikeUUID(value) {
		return value
	}
	for start := 0; start+36 <= len(value); start++ {
		candidate := value[start : start+36]
		if looksLikeUUID(candidate) {
			return candidate
		}
	}
	return ""
}

func hasFailedSecretCheck(items []SecretCheckItem) bool {
	for _, item := range items {
		if strings.EqualFold(item.Status, "FAIL") {
			return true
		}
	}
	return false
}

func hasErroredSecretCheck(items []SecretCheckItem) bool {
	for _, item := range items {
		if strings.EqualFold(item.Status, "ERROR") {
			return true
		}
	}
	return false
}

func (s *JobService) checkImagePullSecrets(ctx context.Context, identity *jobIdentity, secretNames []string) []SecretCheckItem {
	if len(secretNames) == 0 {
		return nil
	}

	namespace := ""
	useHostSecret := false
	vclusterName := ""
	vcNamespace := ""
	if identity != nil {
		vclusterName = strings.TrimSpace(identity.VClusterName)
		vcNamespace = strings.TrimSpace(identity.Namespace)
	}
	if identity != nil && strings.TrimSpace(identity.HostNamespace) != "" {
		namespace = identity.HostNamespace
		useHostSecret = true
	} else if identity != nil {
		namespace = identity.Namespace
	}

	results := make([]SecretCheckItem, 0, len(secretNames))
	for _, secretName := range secretNames {
		var secret *corev1.Secret
		var err error

		switch {
		case useHostSecret:
			secret, err = s.clientset.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
			if err != nil && apierrors.IsNotFound(err) && s.vcClient != nil && identity != nil && vclusterName != "" && vcNamespace != "" {
				secret, err = s.getPlatformSecret(ctx, identity, vcNamespace, secretName)
			}
		case s.vcClient != nil && identity != nil && strings.TrimSpace(identity.VClusterName) != "" && strings.TrimSpace(namespace) != "":
			secret, err = s.getPlatformSecret(ctx, identity, namespace, secretName)
		default:
			err = fmt.Errorf("unable to determine secret namespace")
		}
		if err != nil {
			results = append(results, SecretCheckItem{
				SecretName: secretName,
				Status:     "FAIL",
				Message:    formatSecretCheckMessage(err, secretName, namespace),
			})
			continue
		}

		credentials := decodeDockerCredentials(secret)
		if len(credentials) == 0 {
			results = append(results, SecretCheckItem{
				SecretName: secretName,
				Status:     "FAIL",
				Message:    "secret does not contain docker registry credentials",
			})
			continue
		}

		credential := credentials[0]
		if credential.RegistryHasWhitespace {
			results = append(results, SecretCheckItem{
				SecretName: secretName,
				Registry:   credential.RegistryRaw,
				Username:   credential.Username,
				Password:   credential.Password,
				Status:     "FAIL",
				Message:    fmt.Sprintf("secret 中 registry 地址 %q 两端存在空格，请去掉空格后重试", credential.RegistryRaw),
			})
			continue
		}

		registry := strings.TrimSpace(credential.Registry)
		if registry == "" {
			registry = defaultRegistryHost
		}
		if err := verifyDockerLogin(ctx, registry, credential.Username, credential.Password); err != nil {
			status, message := classifyDockerLoginError(err, registry)
			results = append(results, SecretCheckItem{
				SecretName: secretName,
				Registry:   registry,
				Username:   credential.Username,
				Password:   credential.Password,
				Status:     status,
				Message:    message,
			})
			continue
		}

		results = append(results, SecretCheckItem{
			SecretName: secretName,
			Registry:   registry,
			Username:   credential.Username,
			Password:   credential.Password,
			Status:     "OK",
			Message:    "docker login succeeded",
		})
	}

	return results
}

func formatSecretCheckMessage(err error, secretName string, namespace string) string {
	if err == nil {
		return ""
	}

	message := strings.TrimSpace(err.Error())
	lower := strings.ToLower(message)
	if strings.Contains(lower, "secret") && strings.Contains(lower, "not found") {
		ns := strings.TrimSpace(namespace)
		if ns == "" {
			return fmt.Sprintf("secret %q 在当前 namespace 不存在，镜像密钥检测不通过", secretName)
		}
		return fmt.Sprintf("secret %q 在当前 namespace %q 不存在，镜像密钥检测不通过", secretName, ns)
	}
	return message
}

func verifyDockerLogin(ctx context.Context, registry string, username string, password string) error {
	if strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" {
		return fmt.Errorf("missing username or password")
	}
	if err := preflightDockerRegistry(ctx, registry); err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "rayctl-docker-login-*")
	if err != nil {
		return fmt.Errorf("create temp docker config: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	loginCtx, cancel := context.WithTimeout(ctx, dockerLoginTimeout)
	defer cancel()

	cmd := exec.CommandContext(loginCtx, "docker", "--config", tmpDir, "login", registry, "--username", username, "--password-stdin")
	cmd.Stdin = strings.NewReader(password)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if loginCtx.Err() != nil {
			return fmt.Errorf("docker login exceeded %s: %w", dockerLoginTimeout, loginCtx.Err())
		}
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("%s", message)
	}
	return nil
}

func preflightDockerRegistry(ctx context.Context, registry string) error {
	host, err := dockerRegistryHostname(registry)
	if err != nil {
		return err
	}
	if net.ParseIP(host) != nil || strings.EqualFold(host, "localhost") {
		return nil
	}

	lookupCtx, cancel := context.WithTimeout(ctx, registryLookupTimeout)
	defer cancel()
	if _, err := net.DefaultResolver.LookupHost(lookupCtx, host); err != nil {
		return fmt.Errorf("lookup registry host %q: %w", host, err)
	}
	return nil
}

func dockerRegistryHostname(registry string) (string, error) {
	registry = strings.TrimSpace(registry)
	if registry == "" {
		return "", fmt.Errorf("invalid URL: empty registry address")
	}

	candidate := registry
	if !strings.Contains(candidate, "://") {
		candidate = "//" + candidate
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return "", fmt.Errorf("invalid URL %q: %w", registry, err)
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return "", fmt.Errorf("invalid URL %q: registry host is empty", registry)
	}
	return host, nil
}

func isDockerUnavailableError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "executable file not found") || strings.Contains(message, "no such file or directory")
}

func classifyDockerLoginError(err error, registry string) (string, string) {
	if err == nil {
		return "OK", "docker login succeeded"
	}
	if isDockerRegistryAddressError(err) {
		return "FAIL", fmt.Sprintf(
			"镜像地址错误：镜像仓库 %q 无法解析或地址格式不正确，请检查 imagePullSecret 中的 registry 地址。原始错误：%s",
			strings.TrimSpace(registry),
			strings.TrimSpace(err.Error()),
		)
	}
	if isDockerUnavailableError(err) {
		return "ERROR", strings.TrimSpace(err.Error())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "ERROR", fmt.Sprintf("镜像仓库登录校验超过 %s，暂时无法确认账号密码是否有效", dockerLoginTimeout)
	}
	return "FAIL", strings.TrimSpace(err.Error())
}

func isDockerRegistryAddressError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"no such host",
		"name or service not known",
		"temporary failure in name resolution",
		"server misbehaving",
		"invalid reference format",
		"unsupported protocol scheme",
		"missing port in address",
		"invalid url",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}
