package apphost

// UpdateService 负责应用版本检测、更新通道切换、补丁下载与自动安装。
type UpdateService struct {
	app *App
}

func NewUpdateService(app *App) *UpdateService {
	return &UpdateService{app: app}
}

func (s *UpdateService) GetUpdateChannel() string {
	return s.app.GetUpdateChannel()
}

func (s *UpdateService) CheckUpdate(channel string) (UpdateInfo, error) {
	return s.app.CheckUpdate(channel)
}

func (s *UpdateService) DownloadUpdate(restart bool) error {
	return s.app.DownloadUpdate(restart)
}

func (s *UpdateService) InstallUpdateNow() error {
	return s.app.InstallUpdateNow()
}
