package serve

import (
	"strings"
	"testing"
	"time"
)

func TestSessionRoundTrip(t *testing.T) {
	c := sessionCodec{key: []byte("0123456789abcdef0123456789abcdef")}
	now := time.Now()
	tok := c.mint(now)
	if !c.verify(tok, now) {
		t.Fatal("freshly minted session did not verify")
	}
	if !c.verify(tok, now.Add(sessionTTL-time.Minute)) {
		t.Fatal("session should still verify just before expiry")
	}
}

func TestSessionExpires(t *testing.T) {
	c := sessionCodec{key: []byte("k")}
	tok := c.mint(time.Now())
	if c.verify(tok, time.Now().Add(sessionTTL+time.Minute)) {
		t.Fatal("expired session verified")
	}
}

func TestSessionTamperRejected(t *testing.T) {
	c := sessionCodec{key: []byte("k")}
	tok := c.mint(time.Now())
	body, sig, _ := strings.Cut(tok, ".")
	if c.verify(body+"x."+sig, time.Now()) {
		t.Fatal("tampered payload verified")
	}
	if c.verify(body+"."+sig[:len(sig)-2], time.Now()) {
		t.Fatal("tampered signature verified")
	}
	if c.verify("garbage", time.Now()) {
		t.Fatal("garbage verified")
	}
}

func TestSessionWrongKeyRejected(t *testing.T) {
	tok := sessionCodec{key: []byte("key-one")}.mint(time.Now())
	if (sessionCodec{key: []byte("key-two")}).verify(tok, time.Now()) {
		t.Fatal("session minted with a different key verified")
	}
}
