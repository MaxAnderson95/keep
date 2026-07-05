package serve

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// ceremonyTTL bounds how long a begun WebAuthn ceremony may take.
const ceremonyTTL = 5 * time.Minute

// waUser is the single keep user for WebAuthn purposes. Its handle is the
// random, stable user_id from the state file.
type waUser struct {
	id    []byte
	creds []webauthn.Credential
}

func (u waUser) WebAuthnID() []byte                         { return u.id }
func (u waUser) WebAuthnName() string                       { return "keep" }
func (u waUser) WebAuthnDisplayName() string                { return "keep" }
func (u waUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func (s *Server) waCurrentUser() waUser {
	creds := s.store.Credentials()
	u := waUser{id: s.store.UserID()}
	for _, c := range creds {
		u.creds = append(u.creds, c.Credential)
	}
	return u
}

// relyingParty builds a WebAuthn relying party bound to the origin the client
// is actually using (e.g. the ts.net hostname through the tailnet proxy, or
// localhost during development). Passkeys are origin-bound by design
// (ADR-0005), so deriving the RP from the request keeps both cases working.
func (s *Server) relyingParty(r *http.Request) (*webauthn.WebAuthn, error) {
	host := r.Host
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}
	scheme := "http"
	if requestIsHTTPS(r) {
		scheme = "https"
	}
	return webauthn.New(&webauthn.Config{
		RPID:          hostname,
		RPDisplayName: "keep",
		RPOrigins:     []string{scheme + "://" + host},
	})
}

// ceremonyStore holds in-flight WebAuthn ceremony session data, keyed by an
// unguessable single-use id the client echoes back on finish.
type ceremonyStore struct {
	mu sync.Mutex
	m  map[string]ceremony
}

type ceremony struct {
	data    webauthn.SessionData
	expires time.Time
}

func newCeremonyStore() *ceremonyStore {
	return &ceremonyStore{m: map[string]ceremony{}}
}

func (c *ceremonyStore) put(data webauthn.SessionData) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for id, cer := range c.m {
		if now.After(cer.expires) {
			delete(c.m, id)
		}
	}
	id := base64.RawURLEncoding.EncodeToString(randomBytes(16))
	c.m[id] = ceremony{data: data, expires: now.Add(ceremonyTTL)}
	return id
}

func (c *ceremonyStore) take(id string) (webauthn.SessionData, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cer, ok := c.m[id]
	delete(c.m, id)
	if !ok || time.Now().After(cer.expires) {
		return webauthn.SessionData{}, false
	}
	return cer.data, true
}

// passkeyInfo is the API view of a registered passkey.
type passkeyInfo struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at,omitzero"`
}

func (s *Server) handlePasskeyList(w http.ResponseWriter, r *http.Request) {
	creds := s.store.Credentials()
	out := make([]passkeyInfo, 0, len(creds))
	for _, c := range creds {
		out = append(out, passkeyInfo{
			ID:         c.ID(),
			Name:       c.Name,
			CreatedAt:  c.CreatedAt,
			LastUsedAt: c.LastUsedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": out})
}

func (s *Server) handlePasskeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	wa, err := s.relyingParty(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "webauthn", err.Error())
		return
	}
	user := s.waCurrentUser()
	exclusions := make([]protocol.CredentialDescriptor, 0, len(user.creds))
	for i := range user.creds {
		exclusions = append(exclusions, user.creds[i].Descriptor())
	}
	options, session, err := wa.BeginRegistration(user, webauthn.WithExclusions(exclusions))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "webauthn", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ceremony_id": s.ceremonies.put(*session),
		"options":     options,
	})
}

func (s *Server) handlePasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CeremonyID string          `json:"ceremony_id"`
		Name       string          `json:"name"`
		Credential json.RawMessage `json:"credential"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	session, ok := s.ceremonies.take(body.CeremonyID)
	if !ok {
		writeErr(w, http.StatusBadRequest, "ceremony_expired", "registration ceremony not found or expired; start over")
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(body.Credential))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_credential", err.Error())
		return
	}
	wa, err := s.relyingParty(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "webauthn", err.Error())
		return
	}
	cred, err := wa.CreateCredential(s.waCurrentUser(), session, parsed)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "webauthn", err.Error())
		return
	}
	name := body.Name
	if name == "" {
		name = "passkey"
	}
	if err := s.store.AddCredential(name, *cred); err != nil {
		writeErr(w, http.StatusInternalServerError, "state", err.Error())
		return
	}
	s.log.Info("passkey registered", "name", name)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handlePasskeyDelete(w http.ResponseWriter, r *http.Request) {
	removed, err := s.store.RemoveCredential(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "state", err.Error())
		return
	}
	if !removed {
		writeErr(w, http.StatusNotFound, "unknown_passkey", "no passkey with that id")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handlePasskeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if !loginOriginOK(r) {
		writeErr(w, http.StatusForbidden, "cross_origin", "cross-origin request rejected")
		return
	}
	user := s.waCurrentUser()
	if len(user.creds) == 0 {
		writeErr(w, http.StatusNotFound, "no_passkeys", "no passkeys are registered")
		return
	}
	wa, err := s.relyingParty(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "webauthn", err.Error())
		return
	}
	options, session, err := wa.BeginLogin(user)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "webauthn", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ceremony_id": s.ceremonies.put(*session),
		"options":     options,
	})
}

func (s *Server) handlePasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	if !loginOriginOK(r) {
		writeErr(w, http.StatusForbidden, "cross_origin", "cross-origin request rejected")
		return
	}
	var body struct {
		CeremonyID string          `json:"ceremony_id"`
		Credential json.RawMessage `json:"credential"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	session, ok := s.ceremonies.take(body.CeremonyID)
	if !ok {
		writeErr(w, http.StatusBadRequest, "ceremony_expired", "login ceremony not found or expired; start over")
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(body.Credential))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_credential", err.Error())
		return
	}
	wa, err := s.relyingParty(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "webauthn", err.Error())
		return
	}
	cred, err := wa.ValidateLogin(s.waCurrentUser(), session, parsed)
	if err != nil {
		s.log.Warn("failed passkey login", "tailscale_user", r.Header.Get("Tailscale-User-Login"))
		writeErr(w, http.StatusUnauthorized, "webauthn", err.Error())
		return
	}
	if err := s.store.UpdateCredential(*cred); err != nil {
		s.log.Warn("could not persist passkey counters", "error", err)
	}
	s.setSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
