package sshx

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"ding-ssh/internal/logx"

	"github.com/google/uuid"
	"github.com/pkg/sftp"
)

// openRemoteTempFile keeps a failed or cancelled upload away from its final name.
// The temporary file is created beside the target so rename stays on one filesystem.
func openRemoteTempFile(client *sftp.Client, target string) (string, *sftp.File, error) {
	temp := path.Join(path.Dir(target), ".ding-ssh-"+uuid.NewString()+".tmp")
	file, err := client.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return "", nil, err
	}
	return temp, file, nil
}

// replaceRemoteFile atomically publishes a staged upload when the server supports
// the POSIX rename extension. On older SFTP servers, preserve an existing target
// under a unique backup name until the replacement is in place.
func replaceRemoteFile(client *sftp.Client, temp, target string) error {
	fi, statErr := client.Lstat(target)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if statErr == nil && fi.IsDir() {
		return fmt.Errorf("refusing to replace directory %q with a file", target)
	}
	if statErr == nil && fi.Mode().IsRegular() {
		if err := client.Chmod(temp, fi.Mode().Perm()); err != nil {
			return fmt.Errorf("preserve target permissions: %w", err)
		}
	}
	if _, ok := client.HasExtension("posix-rename@openssh.com"); ok {
		return client.PosixRename(temp, target)
	}
	if errors.Is(statErr, os.ErrNotExist) {
		return client.Rename(temp, target)
	}

	backup := path.Join(path.Dir(target), ".ding-ssh-"+uuid.NewString()+".bak")
	if err := client.Rename(target, backup); err != nil {
		return fmt.Errorf("move existing target aside: %w", err)
	}
	if err := client.Rename(temp, target); err != nil {
		if rollbackErr := client.Rename(backup, target); rollbackErr != nil {
			return fmt.Errorf("publish staged file: %v; restore original file: %w", err, rollbackErr)
		}
		return fmt.Errorf("publish staged file: %w", err)
	}
	if err := client.Remove(backup); err != nil {
		// The destination is already committed. Report a warning but don't present
		// a completed replacement as failed (which could trigger another upload).
		logx.Warnf("[sshx] 上传替换完成但旧文件备份未清理: backup=%s err=%v", backup, err)
	}
	return nil
}

// Both ordinary downloads and confined folder downloads use the same commit
// protocol. Bound os.Root methods keep every filesystem step inside the selection.
type localFileOps struct {
	openFile func(string, int, os.FileMode) (*os.File, error)
	lstat    func(string) (os.FileInfo, error)
	chmod    func(string, os.FileMode) error
	rename   func(string, string) error
	remove   func(string) error
}

func rootedLocalFiles(root *os.Root) localFileOps {
	return localFileOps{root.OpenFile, root.Lstat, root.Chmod, root.Rename, root.Remove}
}

func openLocalTempFile(target string) (string, *os.File, error) {
	return (localFileOps{openFile: os.OpenFile}).openTemp(target)
}

func (files localFileOps) openTemp(target string) (string, *os.File, error) {
	dir := filepath.Dir(target)
	if dir == "." {
		dir = ""
	}
	for attempt := 0; attempt < 3; attempt++ {
		temp := filepath.Join(dir, ".ding-ssh-"+uuid.NewString()+".tmp")
		file, err := files.openFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
		if err == nil {
			return temp, file, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", nil, err
		}
	}
	return "", nil, fmt.Errorf("could not create a unique local temporary file beside %q", target)
}

func replaceLocalFile(temp, target string) error {
	return (localFileOps{lstat: os.Lstat, chmod: os.Chmod, rename: os.Rename, remove: os.Remove}).replace(temp, target)
}

func (files localFileOps) replace(temp, target string) error {
	info, statErr := files.lstat(target)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if statErr == nil && info.IsDir() {
		return fmt.Errorf("refusing to replace directory %q with a file", target)
	}
	if statErr == nil && info.Mode().IsRegular() {
		if err := files.chmod(temp, info.Mode().Perm()); err != nil {
			return fmt.Errorf("preserve target permissions: %w", err)
		}
	}
	renameErr := files.rename(temp, target)
	if renameErr == nil {
		return nil
	}
	_, statErr = files.lstat(target)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return renameErr
		}
		return statErr
	}

	backup := filepath.Join(filepath.Dir(target), ".ding-ssh-"+uuid.NewString()+".bak")
	if err := files.rename(target, backup); err != nil {
		return fmt.Errorf("move existing target aside: %w", err)
	}
	if err := files.rename(temp, target); err != nil {
		if rollbackErr := files.rename(backup, target); rollbackErr != nil {
			return fmt.Errorf("publish staged file: %v; restore original file: %w", err, rollbackErr)
		}
		return fmt.Errorf("publish staged file: %w", err)
	}
	if err := files.remove(backup); err != nil {
		logx.Warnf("[sshx] 下载替换完成但旧文件备份未清理: backup=%s err=%v", backup, err)
	}
	return nil
}
