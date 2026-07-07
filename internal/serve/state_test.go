package serve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"
)

func TestOpenStateStoreCreatesAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keep", "serve-state.json")
	st, err := OpenStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.SigningKey()) != 32 || len(st.UserID()) != 32 {
		t.Fatalf("fresh store has key len %d, user id len %d; want 32/32", len(st.SigningKey()), len(st.UserID()))
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("state file perms = %o, want 600", perm)
	}

	again, err := OpenStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(again.SigningKey()) != string(st.SigningKey()) {
		t.Fatal("signing key not stable across reopen")
	}
}

func TestStateStoreCredentialLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serve-state.json")
	st, err := OpenStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	cred := webauthn.Credential{ID: []byte("cred-id"), PublicKey: []byte("pk")}
	if err := st.AddCredential("iPhone", cred); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	creds := reopened.Credentials()
	if len(creds) != 1 || creds[0].Name != "iPhone" || string(creds[0].Credential.ID) != "cred-id" {
		t.Fatalf("unexpected credentials after reload: %+v", creds)
	}

	removed, err := reopened.RemoveCredential(creds[0].ID())
	if err != nil || !removed {
		t.Fatalf("RemoveCredential = %v, %v; want true, nil", removed, err)
	}
	if got := len(reopened.Credentials()); got != 0 {
		t.Fatalf("credentials after removal = %d, want 0", got)
	}
	if removed, _ := reopened.RemoveCredential("nope"); removed {
		t.Fatal("removing unknown credential reported success")
	}
}
