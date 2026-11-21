package cryptox

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"ding-ssh/internal/appdir"
	"github.com/zalando/go-keyring"
)

func auditVault(t *testing.T) *Vault {
	t.Helper()
	keyring.MockInit() // Never touch the real system keychain in tests.
	dir := t.TempDir()
	t.Setenv(appdir.EnvDevConfigDir, dir)
	vault, err := OpenVault(dir)
	if err != nil {
		t.Fatal(err)
	}
	return vault
}

func TestVaultFailedFieldMigrationPreservesPersistentState(t *testing.T) {
	for _, operation := range []string{"enable", "change", "disable"} {
		t.Run(operation, func(t *testing.T) {
			v := auditVault(t)
			if operation != "enable" {
				if _, _, err := v.EnableMasterPassword("old"); err != nil {
					t.Fatal(err)
				}
			}
			beforeKey, _ := v.Key()
			beforeMeta := v.meta
			sealed, err := v.EncryptField("still-readable-中文")
			if err != nil {
				t.Fatal(err)
			}
			failure := errors.New("synthetic atomic migration failure")
			called := false
			migrate := func(oldKey, newKey []byte) error {
				called = true
				if !bytes.Equal(oldKey, beforeKey) || bytes.Equal(oldKey, newKey) {
					t.Fatal("incorrect migration keys")
				}
				return failure
			}
			switch operation {
			case "enable":
				_, _, err = v.EnableMasterPasswordWithMigration("new", migrate)
			case "change":
				_, _, err = v.ChangeMasterPasswordWithMigration("old", "new", migrate)
			case "disable":
				_, _, err = v.DisableMasterPasswordWithMigration("old", migrate)
			}
			if !called || !errors.Is(err, failure) {
				t.Fatalf("missing migration failure: %v", err)
			}
			key, _ := v.Key()
			if !bytes.Equal(key, beforeKey) || v.meta != beforeMeta {
				t.Fatal("failed migration changed active state")
			}
			reopened, err := OpenVault(v.dir)
			if err != nil {
				t.Fatal(err)
			}
			if reopened.meta.MasterPasswordEnabled {
				if err := reopened.Unlock("old"); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := reopened.DecryptField(sealed); err != nil || got != "still-readable-中文" {
				t.Fatalf("reopen lost data: %q %v", got, err)
			}
		})
	}
}

func TestVaultRejectsMissingVerifier(t *testing.T) {
	v := auditVault(t)
	if _, _, err := v.EnableMasterPassword("correct"); err != nil {
		t.Fatal(err)
	}
	v.meta.VerifierB64 = ""
	if err := v.saveMeta(); err != nil {
		t.Fatal(err)
	}
	// A damaged verifier must never turn password verification into a no-op.
	reopened, err := OpenVault(v.dir)
	if err == nil {
		err = reopened.Unlock("arbitrary-wrong-password")
	}
	if err == nil {
		t.Fatal("missing verifier allowed arbitrary password to unlock")
	}
}

func TestVaultCorruptFileKeyIsNotOverwritten(t *testing.T) {
	v := auditVault(t)
	const damaged = "damaged-key-preserve-for-recovery"
	if err := os.WriteFile(v.fileKeyPath(), []byte(damaged), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenVault(v.dir); err == nil {
		t.Error("corrupt existing key silently replaced")
	}
	got, err := os.ReadFile(v.fileKeyPath())
	if err != nil || string(got) != damaged {
		t.Fatalf("existing key overwritten: %q %v", got, err)
	}
}

func TestVaultRejectsMalformedMasterMetadata(t *testing.T) {
	for _, meta := range []Meta{
		{MasterPasswordEnabled: true},
		{MasterPasswordEnabled: true, SaltB64: "?", VerifierB64: "enc1:invalid"},
	} {
		v := auditVault(t)
		raw, _ := json.Marshal(meta)
		if err := os.WriteFile(v.metaPath(), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenVault(v.dir); err == nil {
			t.Fatal("invalid master metadata accepted")
		}
	}
}

func TestIsolatedVaultMasterPasswordNeverChangesKeyring(t *testing.T) {
	v := auditVault(t)
	const sentinel = "unrelated-real-profile-key"
	if err := keyring.Set(keyringService, keyringUser, sentinel); err != nil {
		t.Fatal(err)
	}
	if _, _, err := v.EnableMasterPassword("audit-master"); err != nil {
		t.Fatal(err)
	}
	if got, err := keyring.Get(keyringService, keyringUser); err != nil || got != sentinel {
		t.Errorf("isolated enable changed keyring: value=%q err=%v", got, err)
	}
	if _, _, err := v.DisableMasterPassword("audit-master"); err != nil {
		t.Fatal(err)
	}
	if got, err := keyring.Get(keyringService, keyringUser); err != nil || got != sentinel {
		t.Errorf("isolated disable changed keyring: value=%q err=%v", got, err)
	}
	reopened, err := OpenVault(v.dir)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := v.Key()
	b, _ := reopened.Key()
	if !bytes.Equal(a, b) {
		t.Fatal("disabling master password did not persist the isolated key")
	}
}

func TestVaultMasterPasswordLifecycle(t *testing.T) {
	v := auditVault(t)
	plain := "password-中文-é"
	original, err := v.EncryptField(plain)
	if err != nil {
		t.Fatal(err)
	}
	oldKey, newKey, err := v.EnableMasterPassword("first")
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := Reencrypt(oldKey, newKey, original)
	if err != nil {
		t.Fatal(err)
	}
	v, err = OpenVault(v.dir)
	if err != nil || !v.Status().NeedsUnlock {
		t.Fatal("vault did not reopen locked", err)
	}
	if _, err := v.DecryptField(encrypted); !errors.Is(err, ErrLocked) {
		t.Fatal("locked decryption accepted", err)
	}
	if err := v.Unlock("wrong"); !errors.Is(err, ErrBadPassword) {
		t.Fatal("wrong password accepted", err)
	}
	if err := v.Unlock("first"); err != nil {
		t.Fatal(err)
	}
	if got, err := v.DecryptField(encrypted); err != nil || got != plain {
		t.Fatal("unlock roundtrip failed", err)
	}
	oldKey, newKey, err = v.ChangeMasterPassword("first", "second")
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err = Reencrypt(oldKey, newKey, encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Unlock("first"); !errors.Is(err, ErrBadPassword) {
		t.Fatal("previous master password accepted", err)
	}
	if err := v.Unlock("second"); err != nil {
		t.Fatal(err)
	}
	oldKey, newKey, err = v.DisableMasterPassword("second")
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err = Reencrypt(oldKey, newKey, encrypted)
	if err != nil {
		t.Fatal(err)
	}
	v, err = OpenVault(v.dir)
	if err != nil || v.Status().NeedsUnlock {
		t.Fatal("disabled vault failed to reopen", err)
	}
	if got, err := v.DecryptField(encrypted); err != nil || got != plain {
		t.Fatal("disabled roundtrip failed", err)
	}
}

func TestVaultFailedMetadataSavePreservesState(t *testing.T) {
	v := auditVault(t)
	oldKey, _ := v.Key()
	oldMeta := v.meta
	if err := os.Remove(v.metaPath()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(v.metaPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := v.EnableMasterPassword("test"); err == nil {
		t.Fatal("expected metadata write to fail")
	}
	if v.meta != oldMeta {
		t.Fatal("failed transition changed in-memory security metadata")
	}
	key, _ := v.Key()
	if !bytes.Equal(key, oldKey) {
		t.Fatal("failed transition changed the active key")
	}
}
