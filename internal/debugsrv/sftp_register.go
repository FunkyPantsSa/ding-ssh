package debugsrv

// M8（SFTP + 终端自动化）的能力位 / 两段式确认登记。
//
// 为什么单独一个文件：地基（caps.go 顶部注释）明确规定「各域不许改内置表 toolCaps，
// 必须在自己文件的 init() 里用 RegisterToolCap / RegisterToolConfirm 登记」。
// sftp_test.go 会重新调用 registerSFTPCaps() 保证用例之间不互相影响
// （RegisterToolCap 幂等，重复登记以最后一次为准），并断言 RegistrationErrors() 为空 ——
// 拼错能力名这类问题不会静默通过。
//
// 能力位划分（与任务规格逐条对应）：
//
//	读类（cap = read，永远允许；但仍全部走审计）：
//	  sftp.list / sftp.stat / sftp.read / sftp.transfers / terminal.expect
//	远端文件写入（fs.remote.write + 两段式确认；路径还要过白名单，见 sftp.go）：
//	  sftp.write（action sftp.write）/ sftp.mkdir / sftp.rename / sftp.remove /
//	  sftp.syncCwd（action sftp.write，非破坏性但同属写类）/ sftp.cancel（action sftp.cancel）
//	终端输入（terminal.input，可逆 / 高频，**不注册** RegisterToolConfirm）：
//	  terminal.run
//
// 为什么 terminal.expect 只登记 CapRead：它不向会话写入任何数据（纯等待 + 读输出增量），
// 按地基规则「读类工具 RegisterToolCap(tool) 不传能力（= read）」处理；
// 会改变远端状态的只有 terminal.run（向终端键入命令）→ CapTerminalInput。

func init() {
	registerSFTPCaps()
}

// registerSFTPCaps 登记 M8 全部工具的能力位与确认动作（幂等，测试可重复调用）。
func registerSFTPCaps() {
	// ---- 读类（永远允许）----
	for _, name := range []string{
		OpSftpList, OpSftpStat, OpSftpRead, OpSftpTransfers, OpTerminalExpect,
	} {
		RegisterToolCap(name, CapRead)
	}

	// ---- 远端文件写入：fs.remote.write + 两段式确认 ----
	registerWrite(OpSftpWrite, CapRemoteFSWrite, OpSftpWrite)
	registerWrite(OpSftpMkdir, CapRemoteFSWrite, OpSftpMkdir)
	registerWrite(OpSftpRename, CapRemoteFSWrite, OpSftpRename)
	registerWrite(OpSftpRemove, CapRemoteFSWrite, OpSftpRemove)
	// 目录同步 = 写类动作（按规格复用 sftp.write 这个 action 名）
	registerWrite(OpSftpSyncCwd, CapRemoteFSWrite, OpSftpWrite)
	// 取消传输：会丢弃一个正在进行的传输（远端 / 本地可能留下半截文件），需要确认
	registerWrite(OpSftpCancel, CapRemoteFSWrite, OpSftpCancel)

	// ---- 终端自动化：写入终端只需 terminal.input，可逆 / 高频 → 不注册确认 ----
	RegisterToolCap(OpTerminalRun, CapTerminalInput)
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
	{OpSftpList, CapRead, ""},
	{OpSftpStat, CapRead, ""},
	{OpSftpRead, CapRead, ""},
	{OpSftpTransfers, CapRead, ""},
	{OpTerminalExpect, CapRead, ""},
	{OpSftpWrite, CapRemoteFSWrite, OpSftpWrite},
	{OpSftpMkdir, CapRemoteFSWrite, OpSftpMkdir},
	{OpSftpRename, CapRemoteFSWrite, OpSftpRename},
	{OpSftpRemove, CapRemoteFSWrite, OpSftpRemove},
	{OpSftpSyncCwd, CapRemoteFSWrite, OpSftpWrite},
	{OpSftpCancel, CapRemoteFSWrite, OpSftpCancel},
	{OpTerminalRun, CapTerminalInput, ""},
}
