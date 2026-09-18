package nativemain

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"unicode/utf8"
)

func identityHash(key []byte, accountID string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(domainIdentity))
	mac.Write([]byte(accountID))
	return hex.EncodeToString(mac.Sum(nil))
}

func identityHint(hash string) string {
	if len(hash) < 8 {
		return "account-" + hash
	}
	return "account-" + hash[:8]
}

func aad(ctx Context, profileID, hash, digest string) []byte {
	raw, _ := json.Marshal([]any{1, ctx.HomeID, profileID, hash, digest})
	return raw
}

func encryptEnvelope(ctx Context, profileID, hash string, env AuthSnapshot, key *Key) (Envelope, error) {
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, fail("INTERNAL_ERROR", "Native-profile operation failed.", 500)
	}
	block, err := aes.NewCipher(key.Raw)
	if err != nil {
		return Envelope{}, fail("INTERNAL_ERROR", "Native-profile operation failed.", 500)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return Envelope{}, fail("INTERNAL_ERROR", "Native-profile operation failed.", 500)
	}
	sealed := gcm.Seal(nil, nonce, env.Raw, aad(ctx, profileID, hash, env.Digest))
	if len(sealed) < 16 {
		return Envelope{}, fail("INTERNAL_ERROR", "Native-profile operation failed.", 500)
	}
	ct, tag := sealed[:len(sealed)-16], sealed[len(sealed)-16:]
	return Envelope{
		Cipher:         "aes-256-gcm",
		KeyRef:         key.Ref,
		Nonce:          base64.StdEncoding.EncodeToString(nonce),
		Ciphertext:     base64.StdEncoding.EncodeToString(ct),
		Tag:            base64.StdEncoding.EncodeToString(tag),
		EnvelopeSHA256: env.Digest,
	}, nil
}

func decryptEnvelope(ctx Context, profileID, hash string, payload Envelope, key *Key) (AuthSnapshot, error) {
	if payload.KeyRef != key.Ref || payload.Cipher != "aes-256-gcm" {
		return AuthSnapshot{}, fail("PROFILE_DECRYPT_FAILED", "The selected native profile could not be decrypted or verified.", 409)
	}
	nonce, err := base64.StdEncoding.DecodeString(payload.Nonce)
	if err != nil {
		return AuthSnapshot{}, fail("PROFILE_DECRYPT_FAILED", "The selected native profile could not be decrypted or verified.", 409)
	}
	ct, err := base64.StdEncoding.DecodeString(payload.Ciphertext)
	if err != nil {
		return AuthSnapshot{}, fail("PROFILE_DECRYPT_FAILED", "The selected native profile could not be decrypted or verified.", 409)
	}
	tag, err := base64.StdEncoding.DecodeString(payload.Tag)
	if err != nil {
		return AuthSnapshot{}, fail("PROFILE_DECRYPT_FAILED", "The selected native profile could not be decrypted or verified.", 409)
	}
	block, err := aes.NewCipher(key.Raw)
	if err != nil {
		return AuthSnapshot{}, fail("PROFILE_DECRYPT_FAILED", "The selected native profile could not be decrypted or verified.", 409)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return AuthSnapshot{}, fail("PROFILE_DECRYPT_FAILED", "The selected native profile could not be decrypted or verified.", 409)
	}
	raw, err := gcm.Open(nil, nonce, append(append([]byte{}, ct...), tag...), aad(ctx, profileID, hash, payload.EnvelopeSHA256))
	if err != nil {
		return AuthSnapshot{}, fail("PROFILE_DECRYPT_FAILED", "The selected native profile could not be decrypted or verified.", 409)
	}
	parsed, err := parseAuthBytes(raw)
	if err != nil || parsed.Digest != payload.EnvelopeSHA256 {
		return AuthSnapshot{}, fail("PROFILE_DECRYPT_FAILED", "The selected native profile could not be decrypted or verified.", 409)
	}
	return parsed, nil
}

func parseAuthBytes(raw []byte) (AuthSnapshot, error) {
	if len(raw) == 0 || len(raw) > maxAuthBytes || !utf8.Valid(raw) {
		return AuthSnapshot{}, fail("AUTH_INVALID", "The native Codex credential envelope is invalid.", 409)
	}
	var body struct {
		AuthMode string `json:"auth_mode"`
		Tokens   struct {
			IDToken      string `json:"id_token"`
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			AccountID    string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.Unmarshal(raw, &body) != nil || (body.AuthMode != "" && body.AuthMode != "chatgpt") {
		return AuthSnapshot{}, fail("AUTH_INVALID", "The native Codex credential envelope is invalid.", 409)
	}
	if body.Tokens.IDToken == "" || body.Tokens.AccessToken == "" || body.Tokens.RefreshToken == "" {
		return AuthSnapshot{}, fail("AUTH_INVALID", "The native Codex credential envelope is invalid.", 409)
	}
	derived := accountIDFromJWT(body.Tokens.IDToken)
	if derived == "" {
		derived = accountIDFromJWT(body.Tokens.AccessToken)
	}
	explicit := strings.TrimSpace(body.Tokens.AccountID)
	if derived != "" && explicit != "" && derived != explicit {
		return AuthSnapshot{}, fail("AUTH_INVALID", "The native Codex credential envelope is invalid.", 409)
	}
	account := derived
	if account == "" {
		account = explicit
	}
	if account == "" {
		return AuthSnapshot{}, fail("AUTH_INVALID", "The native Codex credential envelope is invalid.", 409)
	}
	sum := sha256.Sum256(raw)
	cp := append([]byte(nil), raw...)
	return AuthSnapshot{Text: string(raw), Digest: hex.EncodeToString(sum[:]), AccountID: account, Raw: cp}, nil
}

func accountIDFromJWT(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		ChatGPT string `json:"chatgpt_account_id"`
		Sub     string `json:"sub"`
	}
	if json.Unmarshal(raw, &claims) != nil {
		return ""
	}
	if strings.TrimSpace(claims.ChatGPT) != "" {
		return strings.TrimSpace(claims.ChatGPT)
	}
	return strings.TrimSpace(claims.Sub)
}

func readAuth(path string) (AuthSnapshot, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return AuthSnapshot{}, fail("AUTH_MISSING", "The native Codex credential envelope is missing.", 409)
		}
		return AuthSnapshot{}, fail("AUTH_UNREADABLE", "The native Codex credential envelope is unreadable.", 409)
	}
	return parseAuthBytes(raw)
}

func peekAuth(path string) string {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "missing"
	}
	if err != nil {
		return "unreadable"
	}
	if _, err := readAuth(path); err != nil {
		return "invalid"
	}
	return "ok"
}

func writerTokenHash(token string) string {
	mac := hmac.New(sha256.New, []byte(tokenDomain))
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}
