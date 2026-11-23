package sshx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/pkg/sftp"
)

// ValidateRemoteWritePath checks both the requested path and the server-resolved
// existing ancestors. remove/rename may operate on a final symlink itself; writes
// must never follow one. SFTP cannot eliminate cross-process rename races, so this
// is a confinement check, not a replacement for a restricted remote account.
func ValidateRemoteWritePath(client *sftp.Client, allowlist []string, target string, finalLink bool) error {
	if !path.IsAbs(target) || path.Clean(target) != target || strings.ContainsRune(target, 0) {
		return fmt.Errorf("invalid absolute remote path")
	}
	allowed := func(p, prefix string) bool {
		return prefix != "/" && (p == prefix || strings.HasPrefix(p, strings.TrimRight(prefix, "/")+"/"))
	}
	var canonicalRoots []string
	for _, root := range allowlist {
		root = path.Clean(strings.TrimSpace(root))
		if !path.IsAbs(root) || !allowed(target, root) {
			continue
		}
		if _, err := client.Lstat(root); errors.Is(err, os.ErrNotExist) {
			// Creating the authorized root is safe only when its parent already
			// exists. MkdirAll must not create ancestors outside that root.
			parent, parentErr := client.Stat(path.Dir(root))
			if parentErr != nil || !parent.IsDir() {
				continue
			}
		} else if err != nil {
			return err
		}
		resolved, err := remoteExistingPath(client, root)
		if err != nil {
			return fmt.Errorf("resolve allowed remote root: %w", err)
		}
		canonicalRoots = append(canonicalRoots, resolved)
	}
	if len(canonicalRoots) == 0 {
		return errors.New("remote path is outside the write allowlist")
	}
	check := target
	if fi, err := client.Lstat(target); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if !finalLink {
			return errors.New("refusing to write through a remote symbolic link")
		}
		check = path.Dir(target)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	resolved, err := remoteExistingPath(client, check)
	if err != nil {
		return fmt.Errorf("resolve remote target: %w", err)
	}
	for _, root := range canonicalRoots {
		if allowed(resolved, root) || (finalLink && resolved == path.Dir(root) && target == root) {
			return nil
		}
	}
	return errors.New("remote symbolic link resolves outside the write allowlist")
}

func remoteExistingPath(client *sftp.Client, target string) (string, error) {
	var suffix []string
	for {
		fi, err := client.Lstat(target)
		if err == nil {
			resolved, err := client.RealPath(target)
			if err != nil {
				return "", err
			}
			if len(suffix) > 0 && !fi.IsDir() && fi.Mode()&os.ModeSymlink == 0 {
				return "", errors.New("remote ancestor is not a directory")
			}
			// Some SFTP servers implement REALPATH as lexical normalization. Walk
			// every component explicitly so those servers cannot conceal symlinks.
			resolved, err = resolveRemoteLinks(client, resolved, 0)
			if err != nil {
				return "", err
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = path.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) || target == "/" {
			return "", err
		}
		suffix = append(suffix, path.Base(target))
		target = path.Dir(target)
	}
}

// ValidateRemoteReadPath follows symlinks and checks the actual object against
// client-authorized roots. It does not alter the global unrestricted read API.
func ValidateRemoteReadPath(client *sftp.Client, roots []string, target string) error {
	if !path.IsAbs(target) {
		return errors.New("scoped reads require an absolute path")
	}
	resolved, err := remoteExistingPath(client, path.Clean(target))
	if err != nil {
		return err
	}
	for _, root := range roots {
		root = path.Clean(root)
		if !path.IsAbs(root) || root == "/" {
			continue
		}
		if target != root && !strings.HasPrefix(target, root+"/") {
			continue
		}
		canonical, err := remoteExistingPath(client, root)
		if err != nil {
			return err
		}
		if resolved == canonical || strings.HasPrefix(resolved, canonical+"/") {
			return nil
		}
	}
	return errors.New("remote read resolves outside client authorization")
}

func resolveRemoteLinks(client *sftp.Client, target string, depth int) (string, error) {
	if depth > 40 {
		return "", errors.New("too many remote symbolic links")
	}
	parts := strings.Split(strings.TrimPrefix(path.Clean(target), "/"), "/")
	current := "/"
	for i, part := range parts {
		if part == "" {
			continue
		}
		current = path.Join(current, part)
		fi, err := client.Lstat(current)
		if err != nil {
			return "", err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			link, err := client.ReadLink(current)
			if err != nil {
				return "", err
			}
			if !path.IsAbs(link) {
				link = path.Join(path.Dir(current), link)
			}
			return resolveRemoteLinks(client, path.Join(append([]string{link}, parts[i+1:]...)...), depth+1)
		}
	}
	return current, nil
}

type RemoteWriteOptions struct {
	Append       bool
	CreateDirs   bool
	ExpectedHash string // SHA-256 of entire existing file; "missing" requires no file.
	Allowlist    []string
}

type RemoteWriteResult struct {
	Bytes             int    `json:"bytes"`
	Atomic            bool   `json:"atomic"`
	SHA256            string `json:"sha256"`
	MetadataPreserved bool   `json:"metadataPreserved"`
}

var remoteWriteLocks [64]sync.Mutex

// CreateRemotePrivateFile only creates a new file. It refuses pre-existing
// files and links, applies private permissions before writing any content, and
// removes a partial file on failure. The caller owns cleanup after success.
func CreateRemotePrivateFile(ctx context.Context, client *sftp.Client, target string, data []byte, roots []string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := ValidateRemoteWritePath(client, roots, target, false); err != nil {
		return 0, err
	}
	f, err := client.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return 0, err
	}
	success := false
	defer func() {
		_ = f.Close()
		if !success {
			_ = client.Remove(target)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	n, err := f.Write(data)
	if err != nil {
		return n, err
	}
	if n != len(data) {
		return n, io.ErrShortWrite
	}
	if err := f.Close(); err != nil {
		return n, err
	}
	if err := ctx.Err(); err != nil {
		return n, err
	}
	success = true
	return n, nil
}

// WriteRemoteFile stages a replacement beside its target. Existing files require
// the POSIX atomic-rename extension: unsupported servers retain the old file and
// receive an explicit error. Appends are intentionally reported as non-atomic.
func WriteRemoteFile(ctx context.Context, client *sftp.Client, target string, data []byte, opts RemoteWriteOptions) (RemoteWriteResult, error) {
	key := sha256.Sum256([]byte(fmt.Sprintf("%p/%s", client, target)))
	lock := &remoteWriteLocks[int(key[0])%len(remoteWriteLocks)]
	lock.Lock()
	defer lock.Unlock()
	var out RemoteWriteResult
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if err := ValidateRemoteWritePath(client, opts.Allowlist, target, false); err != nil {
		return out, err
	}
	if err := CheckRemoteHash(client, target, opts.ExpectedHash); err != nil {
		return out, err
	}
	fi, statErr := client.Lstat(target)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return out, statErr
	}
	if statErr == nil && !fi.Mode().IsRegular() {
		return out, errors.New("remote target must be a regular file")
	}
	if opts.CreateDirs {
		if err := client.MkdirAll(path.Dir(target)); err != nil {
			return out, err
		}
	}
	if err := ValidateRemoteWritePath(client, opts.Allowlist, target, false); err != nil {
		return out, err
	}
	if opts.Append {
		f, err := client.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_APPEND)
		if err != nil {
			return out, err
		}
		// SFTP v3 writes carry explicit offsets; not all servers implement the
		// APPEND flag. Seek as well so compatible servers cannot overwrite byte 0.
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			_ = f.Close()
			return out, err
		}
		n, werr := f.Write(data)
		cerr := f.Close()
		out.Bytes = n
		if werr != nil {
			return out, werr
		}
		if cerr != nil {
			return out, cerr
		}
		if n != len(data) {
			return out, io.ErrShortWrite
		}
		return out, nil
	}
	if statErr == nil {
		if _, ok := client.HasExtension("posix-rename@openssh.com"); !ok {
			return out, errors.New("atomic replacement unsupported by remote server; original file preserved")
		}
	}
	temp, f, err := openRemoteTempFile(client, target)
	if err != nil {
		return out, err
	}
	defer func() { _ = f.Close(); _ = client.Remove(temp) }()
	if err := f.Chmod(0o600); err != nil {
		return out, err
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	n, err := f.Write(data)
	if err != nil {
		return out, err
	}
	if n != len(data) {
		return out, io.ErrShortWrite
	}
	if statErr == nil {
		if attrs, ok := fi.Sys().(*sftp.FileStat); ok {
			if err := f.Chown(int(attrs.UID), int(attrs.GID)); err != nil {
				return out, fmt.Errorf("preserve remote owner: %w", err)
			}
		}
		if err := f.Chmod(fi.Mode().Perm()); err != nil {
			return out, err
		}
		out.MetadataPreserved = true
	}
	if _, ok := client.HasExtension("fsync@openssh.com"); ok {
		if err := f.Sync(); err != nil {
			return out, err
		}
	}
	if err := f.Close(); err != nil {
		return out, err
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if err := ValidateRemoteWritePath(client, opts.Allowlist, target, false); err != nil {
		return out, err
	}
	if err := CheckRemoteHash(client, target, opts.ExpectedHash); err != nil {
		return out, err
	}
	if statErr == nil {
		err = client.PosixRename(temp, target)
	} else {
		err = client.Rename(temp, target)
	}
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256(data)
	out.Bytes = n
	out.Atomic = true
	out.SHA256 = hex.EncodeToString(sum[:])
	return out, nil
}

// CheckRemoteHash checks an entire regular file with a bounded read. It is an
// optimistic concurrency check; arbitrary remote processes can still race it.
func CheckRemoteHash(client *sftp.Client, target, expected string) error {
	if expected == "" {
		return nil
	}
	fi, err := client.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		if expected == "missing" {
			return nil
		}
		return errors.New("file conflict: remote file no longer exists")
	}
	if err != nil {
		return err
	}
	if expected == "missing" {
		return errors.New("file conflict: remote file already exists")
	}
	if !fi.Mode().IsRegular() || fi.Size() > 64<<20 {
		return errors.New("conditional write requires a regular file no larger than 64 MiB")
	}
	if len(expected) != 64 {
		return errors.New("expectedHash must be SHA-256 or missing")
	}
	if _, err := hex.DecodeString(expected); err != nil {
		return errors.New("expectedHash must be hexadecimal SHA-256")
	}
	f, err := client.Open(target)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, (64<<20)+1))
	if err != nil {
		return err
	}
	if n > 64<<20 {
		return errors.New("remote file grew beyond hash limit")
	}
	if !strings.EqualFold(expected, hex.EncodeToString(h.Sum(nil))) {
		return errors.New("file conflict: remote content changed; reread and prepare a new change")
	}
	return nil
}

type RemotePatchResult struct {
	ExpectedHash string `json:"expectedHash"`
	NewHash      string `json:"newHash"`
	Matches      int    `json:"matches"`
	BeforeBytes  int    `json:"beforeBytes"`
	AfterBytes   int    `json:"afterBytes"`
	Changed      bool   `json:"changed"`
	DryRun       bool   `json:"dryRun"`
	Applied      bool   `json:"applied"`
	Atomic       bool   `json:"atomic"`
}

// PatchRemoteFile requires an exact match count and a full-content version on
// commit. Preview contains hashes/counts only, avoiding a second content export.
func PatchRemoteFile(ctx context.Context, client *sftp.Client, target, oldText, newText string, matches int, expectedHash string, dry bool, roots []string) (RemotePatchResult, error) {
	var out RemotePatchResult
	if oldText == "" || !utf8.ValidString(oldText) || !utf8.ValidString(newText) || strings.ContainsRune(oldText, 0) || strings.ContainsRune(newText, 0) || matches < 1 || matches > 1000 {
		return out, errors.New("invalid UTF-8 patch or expectedMatches")
	}
	if len(oldText) > 1<<20 || len(newText) > 1<<20 {
		return out, errors.New("patch text exceeds 1 MiB")
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	fi, err := client.Lstat(target)
	if err != nil {
		return out, err
	}
	if !fi.Mode().IsRegular() || fi.Size() > 1<<20 {
		return out, errors.New("patch requires a regular file no larger than 1 MiB")
	}
	f, err := client.Open(target)
	if err != nil {
		return out, err
	}
	data, readErr := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	closeErr := f.Close()
	if readErr != nil {
		return out, readErr
	}
	if closeErr != nil {
		return out, closeErr
	}
	if len(data) > 1<<20 || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return out, errors.New("patch requires UTF-8 content no larger than 1 MiB")
	}
	found := strings.Count(string(data), oldText)
	if found != matches {
		return out, fmt.Errorf("patch conflict: expected %d matches, found %d; file unchanged", matches, found)
	}
	if int64(len(data))+int64(found)*(int64(len(newText))-int64(len(oldText))) > 1<<20 {
		return out, errors.New("patched file would exceed 1 MiB")
	}
	updated := []byte(strings.ReplaceAll(string(data), oldText, newText))
	before := sha256.Sum256(data)
	after := sha256.Sum256(updated)
	out = RemotePatchResult{ExpectedHash: hex.EncodeToString(before[:]), NewHash: hex.EncodeToString(after[:]), Matches: found, BeforeBytes: len(data), AfterBytes: len(updated), Changed: string(data) != string(updated), DryRun: dry}
	if dry {
		return out, nil
	}
	if expectedHash == "" || !strings.EqualFold(expectedHash, out.ExpectedHash) {
		return out, errors.New("patch conflict: expectedHash missing or stale; preview again")
	}
	res, err := WriteRemoteFile(ctx, client, target, updated, RemoteWriteOptions{ExpectedHash: expectedHash, Allowlist: roots})
	if err != nil {
		return out, err
	}
	out.Applied = true
	out.Atomic = res.Atomic
	return out, nil
}
