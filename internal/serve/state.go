package serve

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// state is keep serve's machine-local persistent state (ADR-0005): the
// session-cookie signing key plus registered passkeys. It deliberately lives
// under ~/Library/Application Support/keep — outside ~/.config/keep — so it
// never tangles with the dotfiles-managed Config, and it must never be
// committed anywhere.
type state struct {
	SigningKey  []byte       `json:"signing_key"`
	UserID      []byte       `json:"user_id"`
	Credentials []credential `json:"credentials"`
}

// credential is one registered passkey with display metadata.
type credential struct {
	Name       string              `json:"name"`
	CreatedAt  time.Time           `json:"created_at"`
	LastUsedAt time.Time           `json:"last_used_at,omitzero"`
	Credential webauthn.Credential `json:"credential"`
}

// ID returns the passkey's URL-safe identifier (its credential ID).
func (c credential) ID() string {
	return base64.RawURLEncoding.EncodeToString(c.Credential.ID)
}

// DefaultStatePath is where serve keeps its state unless overridden.
func DefaultStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home: %w", err)
	}
	return filepath.Join(home, "Library", "Application Support", "keep", "serve-state.json"), nil
}

// StateStore loads and persists state with owner-only permissions and atomic
// writes. All mutating methods persist before returning.
type StateStore struct {
	path string

	mu sync.Mutex
	s  state
}

// OpenStateStore reads the state file, creating it (with a fresh signing key
// and user handle) when it does not exist yet.
func OpenStateStore(path string) (*StateStore, error) {
	st := &StateStore{path: path}
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		st.s = state{
			SigningKey: randomBytes(32),
			UserID:     randomBytes(32),
		}
		if err := st.persistLocked(); err != nil {
			return nil, err
		}
		return st, nil
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(data, &st.s); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if len(st.s.SigningKey) == 0 || len(st.s.UserID) == 0 {
		return nil, fmt.Errorf("state file %s is missing its signing key or user id", path)
	}
	return st, nil
}

// SigningKey returns the session-cookie signing key.
func (st *StateStore) SigningKey() []byte {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.SigningKey
}

// UserID returns the stable WebAuthn user handle.
func (st *StateStore) UserID() []byte {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.UserID
}

// Credentials returns a copy of the registered passkeys.
func (st *StateStore) Credentials() []credential {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]credential, len(st.s.Credentials))
	copy(out, st.s.Credentials)
	return out
}

// AddCredential registers a new passkey.
func (st *StateStore) AddCredential(name string, cred webauthn.Credential) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.Credentials = append(st.s.Credentials, credential{
		Name:       name,
		CreatedAt:  time.Now().UTC(),
		Credential: cred,
	})
	return st.persistLocked()
}

// UpdateCredential stores post-login authenticator state (sign counter,
// backup flags) and stamps last-used.
func (st *StateStore) UpdateCredential(cred webauthn.Credential) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := range st.s.Credentials {
		if string(st.s.Credentials[i].Credential.ID) == string(cred.ID) {
			st.s.Credentials[i].Credential = cred
			st.s.Credentials[i].LastUsedAt = time.Now().UTC()
			return st.persistLocked()
		}
	}
	return fmt.Errorf("unknown credential")
}

// RemoveCredential deletes a passkey by its URL-safe id. It reports whether a
// credential was removed.
func (st *StateStore) RemoveCredential(id string) (bool, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := range st.s.Credentials {
		if st.s.Credentials[i].ID() == id {
			st.s.Credentials = append(st.s.Credentials[:i], st.s.Credentials[i+1:]...)
			return true, st.persistLocked()
		}
	}
	return false, nil
}

func (st *StateStore) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(st.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st.s, "", "  ")
	if err != nil {
		return err
	}
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, st.path)
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand never fails on supported platforms; treat it as fatal.
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	return b
}
