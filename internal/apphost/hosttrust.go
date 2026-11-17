package apphost

import "ding-ssh/internal/sshx"

func (a *App) GetHostTrust() ([]sshx.HostTrustRecord, error) { return sshx.ListHostTrust() }
func (a *App) DeleteHostTrust(line int, fingerprint string) error {
	return sshx.DeleteHostTrust(line, fingerprint)
}
func (a *App) ResolveHostTrust(id string, approve, replace bool) error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	return sshx.ResolveHostTrust(id, approve, replace)
}
