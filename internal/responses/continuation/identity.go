package continuation

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"strings"
)

var ErrDurableIdentityUnavailable = errors.New("durable credential identity is unavailable")

const StoreVersion = 4

type Owner struct {
	Provider    string `json:"provider"`
	Destination string `json:"destination"`
	Adapter     string `json:"adapter"`
	Model       string `json:"model"`
	Credential  string `json:"credential"`
}

type ReplayKey struct {
	Thread string `json:"thread"`
	CallID string `json:"call_id"`
	Owner  Owner  `json:"owner"`
}

func NewOwner(provider, destination, adapter, model, credential string) (Owner, error) {
	provider = strings.TrimSpace(provider)
	adapter = strings.TrimSpace(adapter)
	model = strings.TrimSpace(model)
	credential = strings.TrimSpace(credential)
	if provider == "" || adapter == "" || model == "" || credential == "" {
		return Owner{}, fmt.Errorf("continuation owner dimensions are required")
	}
	destinationID, err := DestinationIdentity(destination)
	if err != nil {
		return Owner{}, err
	}
	return Owner{Provider: provider, Destination: destinationID, Adapter: adapter, Model: model, Credential: credential}, nil
}

func (o Owner) Matches(other Owner) bool {
	return o.Provider == other.Provider &&
		o.Destination == other.Destination &&
		o.Adapter == other.Adapter &&
		o.Model == other.Model &&
		o.Credential == other.Credential
}

func DestinationIdentity(raw string) (string, error) {
	canonical, err := canonicalDestination(raw)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(canonical))
	return "dst:" + hex.EncodeToString(digest[:]), nil
}

func canonicalDestination(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("destination is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid destination")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	hostname := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		u.Host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		u.Host = "[" + hostname + "]"
	} else {
		u.Host = hostname
	}
	u.User = nil
	u.Fragment = ""
	cleaned := path.Clean("/" + strings.TrimPrefix(u.EscapedPath(), "/"))
	if cleaned == "/" {
		cleaned = ""
	}
	u.RawPath = ""
	u.Path = cleaned
	return u.String(), nil
}

func DurableKeyIdentity(secret, installationSalt []byte) (string, error) {
	if len(secret) == 0 || len(installationSalt) < 32 {
		return "", ErrDurableIdentityUnavailable
	}
	mac := hmac.New(sha256.New, installationSalt)
	_, _ = mac.Write([]byte("benes/durable-credential/v1\x00"))
	_, _ = mac.Write(secret)
	return "key:" + hex.EncodeToString(mac.Sum(nil)), nil
}

func DurableOAuthIdentity(stableHandle string) (string, error) {
	stableHandle = strings.TrimSpace(stableHandle)
	if stableHandle == "" || strings.ContainsAny(stableHandle, "\r\n\x00") {
		return "", ErrDurableIdentityUnavailable
	}
	return "oauth:" + stableHandle, nil
}

func EphemeralKeyIdentity(secret, processSalt []byte) (string, error) {
	if len(secret) == 0 || len(processSalt) < 32 {
		return "", ErrDurableIdentityUnavailable
	}
	mac := hmac.New(sha256.New, processSalt)
	_, _ = mac.Write([]byte("benes/ephemeral-credential/v1\x00"))
	_, _ = mac.Write(secret)
	return "eph:" + hex.EncodeToString(mac.Sum(nil)), nil
}

func DurableCredential(credential string) bool {
	credential = strings.TrimSpace(credential)
	return strings.HasPrefix(credential, "key:") || strings.HasPrefix(credential, "oauth:")
}
