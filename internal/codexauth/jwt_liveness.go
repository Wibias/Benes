package codexauth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

func jwtVerifiablyLive(token string, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[1] == "" {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		Exp *float64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp == nil {
		return false
	}
	nowSeconds := float64(now.UnixNano()) / float64(time.Second)
	return *claims.Exp > nowSeconds
}
