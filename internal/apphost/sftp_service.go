package apphost

import (
	"ding-ssh/internal/models"
)

// SFTPService 负责远程与本地文件管理、SFTP 目录同步及传输进度。
type SFTPService struct {
	app *App
}

func NewSFTPService(app *App) *SFTPService {
	return &SFTPService{app: app}
}

func (s *SFTPService) SftpList(sessionID, path string) ([]models.SFTPEntry, error) {
	return s.app.SftpList(sessionID, path)
}

func (s *SFTPService) SftpMkdir(sessionID, path string) error {
	return s.app.SftpMkdir(sessionID, path)
}

func (s *SFTPService) SftpRemove(sessionID, path string) error {
	return s.app.SftpRemove(sessionID, path)
}

func (s *SFTPService) SftpRename(sessionID, oldPath, newPath string) error {
	return s.app.SftpRename(sessionID, oldPath, newPath)
}

func (s *SFTPService) SftpUpload(sessionID, localPath, remotePath string) error {
	return s.app.SftpUpload(sessionID, localPath, remotePath)
}

func (s *SFTPService) SftpUploadFolder(sessionID, localDir, remoteDir string) error {
	return s.app.SftpUploadFolder(sessionID, localDir, remoteDir)
}

func (s *SFTPService) SftpDownload(sessionID, remotePath, localPath string) error {
	return s.app.SftpDownload(sessionID, remotePath, localPath)
}

func (s *SFTPService) SftpDownloadFolder(sessionID, remoteDir, localDir string) error {
	return s.app.SftpDownloadFolder(sessionID, remoteDir, localDir)
}

func (s *SFTPService) SftpCancelTransfer(sessionID, direction, name string) error {
	return s.app.SftpCancelTransfer(sessionID, direction, name)
}

func (s *SFTPService) SetSftpPathFromTerminal(sessionID, path string) error {
	return s.app.SetSftpPathFromTerminal(sessionID, path)
}

func (s *SFTPService) SyncSftpToTerminal(sessionID, path string) error {
	return s.app.SyncSftpToTerminal(sessionID, path)
}

func (s *SFTPService) LocalPathIsDir(path string) (bool, error) {
	return s.app.LocalPathIsDir(path)
}

func (s *SFTPService) RevealLocalPath(path string) error {
	return s.app.RevealLocalPath(path)
}

func (s *SFTPService) ReadLocalFileBase64(path string) (string, error) {
	return s.app.ReadLocalFileBase64(path)
}

func (s *SFTPService) WriteLocalFileBase64(path string, dataBase64 string) error {
	return s.app.WriteLocalFileBase64(path, dataBase64)
}

func (s *SFTPService) OpenSFTPWindow(sessionID, title string) error {
	return s.app.OpenSFTPWindow(sessionID, title)
}
