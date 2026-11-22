package debugsrv

import "ding-ssh/internal/logx"

// File content reads, remote mutations, panel navigation and transfer
// cancellation have independent authorization. Only mutations need a one-time
// approved operation token. Interactive terminal input remains powerful and
// requires explicit terminal execution authorization.

func init() {
	registerSFTPCaps()
}

// registerSFTPCaps 登记 M8 全部工具的能力位与确认动作（幂等，测试可重复调用）。
func registerSFTPCaps() {
	// File reads are a distinct authorization boundary from app diagnostics.
	for _, name := range []string{OpSftpList, OpSftpStat, OpSftpRead, OpSftpDownload, OpSftpPatchPreview} {
		RegisterToolCap(name, CapRemoteFSRead)
	}
	RegisterToolCap(OpSftpTransfers, CapRead)
	RegisterToolCap(OpTerminalExpect, CapTerminalRead)
	readNames := []string{OpSftpList, OpSftpStat, OpSftpRead, OpSftpDownload, OpSftpPatchPreview, OpSftpTransfers, OpTerminalExpect}

	// ---- 远端文件写入：fs.remote.write + 两段式确认 ----
	writes := 2
	registerWrite(OpSftpWrite, CapRemoteFSWrite, OpSftpWrite)
	writes++
	registerWrite(OpSftpMkdir, CapRemoteFSWrite, OpSftpMkdir)
	writes++
	registerWrite(OpSftpRename, CapRemoteFSWrite, OpSftpRename)
	writes++
	registerWrite(OpSftpRemove, CapRemoteFSWrite, OpSftpRemove)
	writes++
	registerWrite(OpSftpUpload, CapRemoteFSWrite, OpSftpUpload)
	registerWrite(OpSftpPatch, CapRemoteFSWrite, OpSftpPatch)
	RegisterToolCap(OpSftpPatch, CapRemoteFSWrite, CapRemoteFSRead)
	RegisterToolCap(OpSftpSyncCwd, CapUIWrite)
	UnregisterToolConfirm(OpSftpSyncCwd)
	RegisterToolCap(OpSftpCancel, CapTransferCancel)
	UnregisterToolConfirm(OpSftpCancel)

	// Interactive input is explicitly authorized; it is not inherently reversible.
	RegisterToolCap(OpTerminalRun, CapTerminalInput)

	logx.Infof("[debugsrv/sftp] 注册 SFTP 工具能力位: total=%d read=%d write=%d terminalInput=1",
		len(sftpToolNames), len(readNames), writes)
}

// sftpWantRegistry 规格表：工具 → 能力位 → 确认 action（空 action = 不需要 token）。
//
// 供 sftp_test.go 断言（与 appctl_register.go 的 appctlWantRegistry 同一做法），
// 写在这里而不是测试文件里，避免「登记表」与「规格」两处漂移。
var sftpWantRegistry = []struct {
	Tool   string
	Cap    Capability
	Action string
}{
	{OpSftpList, CapRemoteFSRead, ""},
	{OpSftpStat, CapRemoteFSRead, ""},
	{OpSftpRead, CapRemoteFSRead, ""},
	{OpSftpTransfers, CapRead, ""},
	{OpTerminalExpect, CapTerminalRead, ""},
	{OpSftpWrite, CapRemoteFSWrite, OpSftpWrite},
	{OpSftpMkdir, CapRemoteFSWrite, OpSftpMkdir},
	{OpSftpRename, CapRemoteFSWrite, OpSftpRename},
	{OpSftpRemove, CapRemoteFSWrite, OpSftpRemove},
	{OpSftpSyncCwd, CapUIWrite, ""},
	{OpSftpCancel, CapTransferCancel, ""},
	{OpTerminalRun, CapTerminalInput, ""},
	{OpSftpPatchPreview, CapRemoteFSRead, ""},
	{OpSftpPatch, CapRemoteFSWrite, OpSftpPatch},
	{OpSftpUpload, CapRemoteFSWrite, OpSftpUpload},
	{OpSftpDownload, CapRemoteFSRead, ""},
}
