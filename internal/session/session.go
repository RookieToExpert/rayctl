package session

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultSessionFileName = "session.json"
const consoleClientID = "4e414854-7200-509f-b960-f8b0dbbbf331"

type Store struct {
	Profiles map[string]ProfileSession `json:"profiles"`
}

type ProfileSession struct {
	Username     string `json:"username"`
	TenantCode   string `json:"tenant_code,omitempty"`
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    string `json:"expires_at"`
	SigninURL    string `json:"signin_url"`
}

type loginResponse struct {
	AccessToken  string `json:"access_token"`
	Token        string `json:"token"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Data         struct {
		AccessToken  string `json:"access_token"`
		Token        string `json:"token"`
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	} `json:"data"`
}

type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	KTY string `json:"kty"`
	KID string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type LoginOptions struct {
	Debug       bool
	DebugWriter io.Writer
}

type LoginOption func(*LoginOptions)

func WithDebug(writer io.Writer) LoginOption {
	return func(options *LoginOptions) {
		options.Debug = true
		options.DebugWriter = writer
	}
}

func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".rayctl", defaultSessionFileName)
	}
	return filepath.Join(home, ".rayctl", defaultSessionFileName)
}

func Load(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath()
	}
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			return &Store{Profiles: map[string]ProfileSession{}}, nil
		}
		return nil, err
	}
	var store Store
	if err := json.Unmarshal(content, &store); err != nil {
		return nil, err
	}
	if store.Profiles == nil {
		store.Profiles = map[string]ProfileSession{}
	}
	return &store, nil
}

func Save(path string, store *Store) error {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath()
	}
	if store == nil {
		store = &Store{Profiles: map[string]ProfileSession{}}
	}
	if store.Profiles == nil {
		store.Profiles = map[string]ProfileSession{}
	}
	content, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Clean(path), append(content, '\n'), 0o600)
}

func Login(ctx context.Context, signinURL string, iamBaseURL string, username string, password string, tenantCode string, opts ...LoginOption) (*ProfileSession, error) {
	options := resolveLoginOptions(opts...)
	signinURL = strings.TrimSpace(signinURL)
	iamBaseURL = strings.TrimRight(strings.TrimSpace(iamBaseURL), "/")
	username = strings.TrimSpace(username)
	tenantCode = strings.TrimSpace(tenantCode)
	if signinURL == "" {
		return nil, fmt.Errorf("signin url is required")
	}
	if iamBaseURL == "" {
		return nil, fmt.Errorf("iam base url is required")
	}
	if username == "" || password == "" {
		return nil, fmt.Errorf("username and password are required")
	}

	tokenURL, authURL, redirectURI, verifier, state, err := buildPKCEURLs(signinURL)
	if err != nil {
		return nil, err
	}
	debugf(options, "auth urls: signin=%s iam=%s redirect_uri=%s", strings.TrimRight(signinURL, "/"), iamBaseURL, redirectURI)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	challenge, err := requestLoginChallenge(ctx, client, authURL)
	if err != nil {
		return nil, err
	}
	debugf(options, "challenge: ok len=%d", len(challenge))
	challengeValid, challengePlatform, err := checkLoginChallenge(ctx, client, iamBaseURL, challenge)
	if err != nil {
		return nil, err
	}
	debugf(options, "checkChallenge: is_valid=%t platform=%s", challengeValid, emptyDebugValue(challengePlatform))
	if !challengeValid {
		return nil, fmt.Errorf("login challenge is invalid")
	}
	encryptedPassword, err := encryptPasswordForSignin(ctx, client, signinURL, password)
	if err != nil {
		return nil, err
	}
	debugf(options, "jwe header: %s", jweHeader(encryptedPassword))
	debugf(options, "login payload keys: challenge,is_encrypt,login_type,password,username")
	needCaptcha, err := checkAuthNeedCaptcha(ctx, client, iamBaseURL, tenantCode, username)
	if err != nil {
		return nil, err
	}
	debugf(options, "needCaptcha: result=%t", needCaptcha)
	if needCaptcha {
		return nil, fmt.Errorf("login requires captcha; please login in console and pass token with --bearer-token or RAYCTL_BEARER_TOKEN")
	}
	redirectTo, err := submitIAMLogin(ctx, client, iamBaseURL, "", username, encryptedPassword, true, challenge)
	if err != nil {
		return nil, err
	}
	code, err := followRedirectForCode(ctx, client, redirectTo)
	if err != nil {
		return nil, err
	}

	respBody, statusCode, err := exchangeAuthorizationCode(ctx, client, tokenURL, code, redirectURI, verifier, state)
	if err != nil {
		return nil, err
	}
	if statusCode < 200 || statusCode >= 300 {
		return nil, fmt.Errorf("login %s returned %d: %s", tokenURL, statusCode, strings.TrimSpace(string(respBody)))
	}
	var payload loginResponse
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return nil, fmt.Errorf("decode login response: %w", err)
	}
	token := firstNonEmpty(payload.AccessToken, payload.Token, payload.Data.AccessToken, payload.Data.Token)
	if token == "" {
		return nil, fmt.Errorf("login response does not contain access_token")
	}

	expiresAt := tokenExpiresAt(token)
	if expiresAt.IsZero() {
		expiresIn := payload.ExpiresIn
		if expiresIn == 0 {
			expiresIn = payload.Data.ExpiresIn
		}
		if expiresIn > 0 {
			expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
		}
	}

	return &ProfileSession{
		Username:     username,
		TenantCode:   tenantCode,
		AccessToken:  token,
		IDToken:      firstNonEmpty(payload.IDToken, payload.Data.IDToken),
		RefreshToken: firstNonEmpty(payload.RefreshToken, payload.Data.RefreshToken),
		ExpiresAt:    formatExpiresAt(expiresAt),
		SigninURL:    signinURL,
	}, nil
}

func NewBearerSession(signinURL string, username string, tenantCode string, bearerToken string) ProfileSession {
	token := strings.TrimPrefix(strings.TrimSpace(bearerToken), "Bearer ")
	expiresAt := tokenExpiresAt(token)
	return ProfileSession{
		Username:    strings.TrimSpace(username),
		TenantCode:  strings.TrimSpace(tenantCode),
		AccessToken: token,
		ExpiresAt:   formatExpiresAt(expiresAt),
		SigninURL:   strings.TrimSpace(signinURL),
	}
}

func resolveLoginOptions(opts ...LoginOption) LoginOptions {
	options := LoginOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	if options.Debug && options.DebugWriter == nil {
		options.DebugWriter = os.Stderr
	}
	return options
}

func debugf(options LoginOptions, format string, args ...any) {
	if !options.Debug {
		return
	}
	writer := options.DebugWriter
	if writer == nil {
		writer = os.Stderr
	}
	fmt.Fprintf(writer, "auth login debug: "+format+"\n", args...)
}

func buildPKCEURLs(signinURL string) (tokenURL string, authURL string, redirectURI string, verifier string, state string, err error) {
	parsed, err := url.Parse(signinURL)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("parse signin url: %w", err)
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "https"
	}
	parsed.Path = "/oauth2/token"
	parsed.RawQuery = ""
	tokenURL = parsed.String()

	redirectURI = redirectURIFromSigninHost(parsed)
	verifier, err = randomURLSafeString(48)
	if err != nil {
		return "", "", "", "", "", err
	}
	challengeBytes := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeBytes[:])
	state, err = randomURLSafeString(24)
	if err != nil {
		return "", "", "", "", "", err
	}

	parsed.Path = "/oauth2/auth"
	query := parsed.Query()
	query.Set("response_type", "code")
	query.Set("client_id", consoleClientID)
	query.Set("code_challenge_method", "S256")
	query.Set("code_challenge", challenge)
	query.Set("redirect_uri", redirectURI)
	query.Set("scope", "openid offline offline_access")
	query.Set("state", state)
	parsed.RawQuery = query.Encode()
	authURL = parsed.String()
	return tokenURL, authURL, redirectURI, verifier, state, nil
}

func redirectURIFromSigninHost(signinURL *url.URL) string {
	host := signinURL.Host
	switch {
	case strings.HasPrefix(host, "signin."):
		host = "console." + strings.TrimPrefix(host, "signin.")
	case strings.HasPrefix(host, "signin-"):
		host = "console-" + strings.TrimPrefix(host, "signin-")
	default:
		host = strings.Replace(host, "signin", "console", 1)
	}
	return (&url.URL{Scheme: "https", Host: host, Path: "/home"}).String()
}

func requestLoginChallenge(ctx context.Context, client *http.Client, authURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("request auth challenge returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	location := resp.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("request auth challenge returned %d without Location", resp.StatusCode)
	}
	challenge := queryValueFromLocation(location, "login_challenge")
	if challenge == "" {
		return "", fmt.Errorf("auth redirect does not contain login_challenge: %s", location)
	}
	_ = warmLoginPage(ctx, client, authURL, location)
	return challenge, nil
}

func warmLoginPage(ctx context.Context, client *http.Client, authURL string, location string) error {
	base, _ := url.Parse(authURL)
	current := strings.TrimSpace(location)
	for i := 0; i < 4; i++ {
		target, err := url.Parse(current)
		if err != nil {
			return err
		}
		if base != nil {
			target = base.ResolveReference(target)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode < 300 || resp.StatusCode >= 400 {
			return nil
		}
		next := resp.Header.Get("Location")
		if next == "" {
			return nil
		}
		current = next
	}
	return nil
}

func encryptPasswordForSignin(ctx context.Context, client *http.Client, signinURL string, password string) (string, error) {
	key, err := fetchSigninPublicKey(ctx, client, signinURL)
	if err != nil {
		return "", err
	}
	encrypted, err := encryptJWECompact(key, []byte(password))
	if err != nil {
		return "", fmt.Errorf("encrypt password: %w", err)
	}
	return encrypted, nil
}

func fetchSigninPublicKey(ctx context.Context, client *http.Client, signinURL string) (*rsa.PublicKey, error) {
	parsed, err := url.Parse(signinURL)
	if err != nil {
		return nil, fmt.Errorf("parse signin url: %w", err)
	}
	parsed.Path = "/.well-known/jwks.json"
	parsed.RawQuery = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("get signin jwks returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var payload jwksResponse
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return nil, fmt.Errorf("decode signin jwks: %w", err)
	}
	for _, item := range payload.Keys {
		if item.KID != "public:hydra.openid.id-token" || strings.ToUpper(item.KTY) != "RSA" {
			continue
		}
		return rsaPublicKeyFromJWK(item)
	}
	return nil, fmt.Errorf("signin jwks does not contain public:hydra.openid.id-token")
}

func rsaPublicKeyFromJWK(item jwkKey) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(item.N)
	if err != nil {
		return nil, fmt.Errorf("decode jwk n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(item.E)
	if err != nil {
		return nil, fmt.Errorf("decode jwk e: %w", err)
	}
	eValue := new(big.Int).SetBytes(eBytes).Int64()
	if eValue <= 0 || eValue > int64(^uint(0)>>1) {
		return nil, fmt.Errorf("invalid jwk exponent")
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(eValue),
	}, nil
}

func encryptJWECompact(publicKey *rsa.PublicKey, plaintext []byte) (string, error) {
	protected := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RSA-OAEP","enc":"A256GCM"}`))
	cek := make([]byte, 32)
	if _, err := rand.Read(cek); err != nil {
		return "", err
	}
	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	encryptedKey, err := rsa.EncryptOAEP(sha1.New(), rand.Reader, publicKey, cek, nil)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, iv, plaintext, []byte(protected))
	tagSize := gcm.Overhead()
	if len(sealed) < tagSize {
		return "", fmt.Errorf("encrypted payload is shorter than tag")
	}
	ciphertext := sealed[:len(sealed)-tagSize]
	tag := sealed[len(sealed)-tagSize:]
	return strings.Join([]string{
		protected,
		base64.RawURLEncoding.EncodeToString(encryptedKey),
		base64.RawURLEncoding.EncodeToString(iv),
		base64.RawURLEncoding.EncodeToString(ciphertext),
		base64.RawURLEncoding.EncodeToString(tag),
	}, "."), nil
}

func jweHeader(compact string) string {
	parts := strings.Split(compact, ".")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "-"
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Sprintf("decode failed: %v", err)
	}
	return string(header)
}

func checkLoginChallenge(ctx context.Context, client *http.Client, iamBaseURL string, challenge string) (bool, string, error) {
	u, err := url.Parse(strings.TrimRight(iamBaseURL, "/"))
	if err != nil {
		return false, "", fmt.Errorf("parse iam url: %w", err)
	}
	u.Path = "/iam/authn/v1/auth/checkChallenge"
	query := u.Query()
	query.Set("challenge", challenge)
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false, "", err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	resp, err := client.Do(req)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, "", fmt.Errorf("check challenge returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var payload struct {
		IsValid  bool   `json:"is_valid"`
		Platform string `json:"platform"`
		Data     struct {
			IsValid  bool   `json:"is_valid"`
			Platform string `json:"platform"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return false, "", fmt.Errorf("decode challenge response: %w", err)
	}
	return payload.IsValid || payload.Data.IsValid, firstNonEmpty(payload.Platform, payload.Data.Platform), nil
}

func checkAuthNeedCaptcha(ctx context.Context, client *http.Client, iamBaseURL string, tenantCode string, username string) (bool, error) {
	if strings.TrimSpace(tenantCode) == "" {
		tenantCode = username
	}
	u, err := url.Parse(strings.TrimRight(iamBaseURL, "/"))
	if err != nil {
		return false, fmt.Errorf("parse iam url: %w", err)
	}
	u.Path = "/iam/authn/v1/auth/needCaptcha"
	query := u.Query()
	query.Set("identifier", username)
	query.Set("tenant_code", tenantCode)
	query.Set("region_code", "")
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("check captcha returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var payload struct {
		Result bool `json:"result"`
		Data   struct {
			Result bool `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return false, fmt.Errorf("decode captcha response: %w", err)
	}
	return payload.Result || payload.Data.Result, nil
}

func emptyDebugValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func submitIAMLogin(ctx context.Context, client *http.Client, iamBaseURL string, tenantCode string, username string, password string, passwordEncrypted bool, challenge string) (string, error) {
	u, err := url.Parse(strings.TrimRight(iamBaseURL, "/"))
	if err != nil {
		return "", fmt.Errorf("parse iam url: %w", err)
	}
	u.Path = "/iam/authn/v1/auth/login"
	payload := map[string]any{
		"username":   username,
		"password":   password,
		"login_type": "username",
		"challenge":  challenge,
		"is_encrypt": passwordEncrypted,
	}
	if strings.TrimSpace(tenantCode) != "" {
		payload["tenant_code"] = strings.TrimSpace(tenantCode)
		delete(payload, "login_type")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		location := resp.Header.Get("Location")
		if location == "" {
			return "", fmt.Errorf("iam login returned %d without Location", resp.StatusCode)
		}
		return location, nil
	}
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("iam login returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	redirect := extractRedirectURL(respBody)
	if redirect == "" {
		return "", fmt.Errorf("iam login response does not contain redirect uri: %s", strings.TrimSpace(string(respBody)))
	}
	return redirect, nil
}

func followRedirectForCode(ctx context.Context, client *http.Client, redirectTo string) (string, error) {
	current := strings.TrimSpace(redirectTo)
	for i := 0; i < 12; i++ {
		if code := queryValueFromLocation(current, "code"); code != "" {
			return code, nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Accept", "application/json, text/plain, */*")
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode < 300 || resp.StatusCode >= 400 {
			return "", fmt.Errorf("login redirect ended at %s with status %d before code", current, resp.StatusCode)
		}
		next := resp.Header.Get("Location")
		if next == "" {
			return "", fmt.Errorf("login redirect returned %d without Location", resp.StatusCode)
		}
		base, _ := url.Parse(current)
		parsedNext, err := url.Parse(next)
		if err == nil && base != nil {
			next = base.ResolveReference(parsedNext).String()
		}
		current = next
	}
	return "", fmt.Errorf("login redirect exceeded maximum hops")
}

func exchangeAuthorizationCode(ctx context.Context, client *http.Client, tokenURL string, code string, redirectURI string, verifier string, state string) ([]byte, int, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("client_id", consoleClientID)
	values.Set("code", code)
	values.Set("redirect_uri", redirectURI)
	values.Set("code_verifier", verifier)
	values.Set("state", state)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, nil
}

func extractRedirectURL(body []byte) string {
	var payload struct {
		RedirectURI string `json:"redirect_uri"`
		RedirectURL string `json:"redirect_url"`
		RedirectTo  string `json:"redirect_to"`
		Redirect    string `json:"redirect"`
		Location    string `json:"location"`
		Data        struct {
			RedirectURI string `json:"redirect_uri"`
			RedirectURL string `json:"redirect_url"`
			RedirectTo  string `json:"redirect_to"`
			Redirect    string `json:"redirect"`
			Location    string `json:"location"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return firstNonEmpty(
		payload.RedirectURI,
		payload.RedirectURL,
		payload.RedirectTo,
		payload.Redirect,
		payload.Location,
		payload.Data.RedirectURI,
		payload.Data.RedirectURL,
		payload.Data.RedirectTo,
		payload.Data.Redirect,
		payload.Data.Location,
	)
}

func queryValueFromLocation(location string, key string) string {
	parsed, err := url.Parse(strings.TrimSpace(location))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Query().Get(key))
}

func randomURLSafeString(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func ValidToken(item ProfileSession) (string, bool) {
	token := firstNonEmpty(item.AccessToken, item.IDToken)
	if token == "" {
		return "", false
	}
	expiresAt, ok := parseExpiresAt(item.ExpiresAt)
	if ok && time.Now().After(expiresAt.Add(-1*time.Minute)) {
		return "", false
	}
	return strings.TrimPrefix(token, "Bearer "), true
}

func ValidAccessToken(item ProfileSession) (string, bool) {
	token := item.AccessToken
	if token == "" {
		return "", false
	}
	expiresAt, ok := parseExpiresAt(item.ExpiresAt)
	if ok && time.Now().After(expiresAt.Add(-1*time.Minute)) {
		return "", false
	}
	return strings.TrimPrefix(token, "Bearer "), true
}

func TokenStatus(item ProfileSession) (string, string) {
	token := firstNonEmpty(item.AccessToken, item.IDToken)
	if token == "" {
		return "missing", "-"
	}
	expiresAt, ok := parseExpiresAt(item.ExpiresAt)
	if !ok {
		return "valid", "unknown"
	}
	if time.Now().After(expiresAt.Add(-1 * time.Minute)) {
		return "expired", expiresAt.In(time.FixedZone("UTC+8", 8*60*60)).Format("2006-01-02 15:04:05")
	}
	return "valid", expiresAt.In(time.FixedZone("UTC+8", 8*60*60)).Format("2006-01-02 15:04:05")
}

func tokenExpiresAt(token string) time.Time {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) < 2 {
		return time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp <= 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0)
}

func formatExpiresAt(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func parseExpiresAt(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
