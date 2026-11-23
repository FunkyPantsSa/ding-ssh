package cryptox

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"testing"

	"ding-ssh/internal/appdir"
	"github.com/zalando/go-keyring"
)

func TestVaultFileFallbackSurvivesKeyringRecovery(t *testing.T) {
	t.Setenv(appdir.EnvDevConfigDir, "")
	keyring.MockInitWithError(errors.New("synthetic keyring outage"))
	t.Cleanup(keyring.MockInit)
	dir := t.TempDir()
	v, err := OpenVault(dir)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := v.EncryptField("fallback-é中文")
	if err != nil {
		t.Fatal(err)
	}
	if err := v.MarkMigrated(); err != nil {
		t.Fatal(err)
	}
	keyring.MockInit()
	reopened, err := OpenVault(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := reopened.DecryptField(sealed); err != nil || got != "fallback-é中文" {
		t.Fatalf("keyring recovery replaced the file key: %q %v", got, err)
	}
}

func TestVaultKeyringOutageDoesNotInventReplacementKey(t *testing.T) {
	t.Setenv(appdir.EnvDevConfigDir, "")
	keyring.MockInit()
	t.Cleanup(keyring.MockInit)
	dir := t.TempDir()
	v, err := OpenVault(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.MarkMigrated(); err != nil {
		t.Fatal(err)
	}
	keyring.MockInitWithError(errors.New("synthetic locked keyring"))
	if _, err := OpenVault(dir); err == nil {
		t.Error("opened existing keyring vault with a new unrelated key")
	}
	if _, err := os.Stat(v.fileKeyPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created replacement file key: %v", err)
	}
}

func TestVaultLegacyFileKeyRemainsAuthoritative(t *testing.T) {
	t.Setenv(appdir.EnvDevConfigDir, "")
	keyring.MockInit()
	t.Cleanup(keyring.MockInit)
	dir := t.TempDir()
	legacy := &Vault{dir: dir}
	key, err := legacy.loadOrCreateFileKey()
	if err != nil {
		t.Fatal(err)
	}
	legacy.meta.MigratedAt = 1
	if err := legacy.saveMeta(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenVault(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Key()
	if err != nil || !bytes.Equal(got, key) {
		t.Fatal("legacy file key was replaced", err)
	}
}

func TestVaultMissingRecordedKeyIsNotRecreated(t *testing.T) {
	for _, source := range []string{"file", "keyring"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv(appdir.EnvDevConfigDir, "")
			if source == "file" {
				keyring.MockInitWithError(errors.New("synthetic outage"))
			} else {
				keyring.MockInit()
			}
			t.Cleanup(keyring.MockInit)
			v, err := OpenVault(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if source == "file" {
				if err := os.Remove(v.fileKeyPath()); err != nil {
					t.Fatal(err)
				}
			}
			keyring.MockInit() // Removes the mock keyring key and makes a new one possible.
			if _, err := OpenVault(v.dir); err == nil {
				t.Error("missing recorded key was silently recreated")
			}
			if _, err := os.Stat(v.fileKeyPath()); !errors.Is(err, os.ErrNotExist) {
				t.Error("replacement file key was created", err)
			}
			if _, err := keyring.Get(keyringService, keyringUser); !errors.Is(err, keyring.ErrNotFound) {
				t.Error("replacement keyring key was created", err)
			}
		})
	}
}

func TestVaultLegacyConflictingKeysArePreservedAndRejected(t *testing.T) {
	t.Setenv(appdir.EnvDevConfigDir, "")
	keyring.MockInit()
	t.Cleanup(keyring.MockInit)
	v := &Vault{dir: t.TempDir()}
	fileKey, err := v.loadOrCreateFileKey()
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := NewMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(otherKey)
	if err := keyring.Set(keyringService, keyringUser, encoded); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenVault(v.dir); err == nil {
		t.Error("ambiguous legacy key sources were silently selected")
	}
	got, err := v.loadFileKey()
	if err != nil || !bytes.Equal(got, fileKey) {
		t.Error("file key changed", err)
	}
	stored, err := keyring.Get(keyringService, keyringUser)
	if err != nil || stored != encoded {
		t.Error("keyring key changed", err)
	}
}

func TestVaultCorruptKeyringEntryIsNotReplacedOrBypassed(t *testing.T) {
	t.Setenv(appdir.EnvDevConfigDir, "")
	keyring.MockInit()
	t.Cleanup(keyring.MockInit)
	if err := keyring.Set(keyringService, keyringUser, "damaged"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := OpenVault(dir); err == nil {
		t.Error("corrupt keyring entry was bypassed")
	}
	if _, err := os.Stat((&Vault{dir: dir}).fileKeyPath()); !errors.Is(err, os.ErrNotExist) {
		t.Error("replacement file key created", err)
	}
	if got, err := keyring.Get(keyringService, keyringUser); err != nil || got != "damaged" {
		t.Error("keyring entry overwritten", err)
	}
}

func TestVaultLegacyIsolatedMissingKeyIsNotRecreated(t *testing.T) {
	v := auditVault(t)
	v.meta.KeySource = "" // Metadata written by the previous version.
	if err := v.saveMeta(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(v.fileKeyPath()); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenVault(v.dir); err == nil {
		t.Error("missing legacy isolated key was recreated")
	}
	if _, err := os.Stat(v.fileKeyPath()); !errors.Is(err, os.ErrNotExist) {
		t.Error("replacement key was created", err)
	}
}
