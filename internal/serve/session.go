package serve

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// sessionTTL is how long a browser session lives (W7). Sessions are stateless
// signed cookies, so bouncing serve does not invalidate them.
const sessionTTL = 30 * 24 * time.Hour

// sessionCookie is the browser session cookie name.
const sessionCookie = "keep_session"

// sessionCodec mints and verifies stateless signed session tokens:
// base64url(payload) + "." + base64url(HMAC-SHA256(payload)).
type sessionCodec struct {
	key []byte
}

type sessionPayload struct {
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
	Nonce     string `json:"n"`
}

func (c sessionCodec) mint(now time.Time) string {
	payload, err := json.Marshal(sessionPayload{
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(sessionTTL).Unix(),
		Nonce:     base64.RawURLEncoding.EncodeToString(randomBytes(8)),
	})
	if err != nil {
		panic(err) // marshaling a flat struct cannot fail
	}
	return base64.RawURLEncoding.EncodeToString(payload) + "." + c.sign(payload)
}

func (c sessionCodec) verify(token string, now time.Time) bool {
	body, sig, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return false
	}
	if !hmac.Equal([]byte(c.sign(payload)), []byte(sig)) {
		return false
	}
	var p sessionPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return false
	}
	return now.Unix() < p.ExpiresAt
}

func (c sessionCodec) sign(payload []byte) string {
	mac := hmac.New(sha256.New, c.key)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
