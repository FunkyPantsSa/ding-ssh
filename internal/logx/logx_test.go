package logx

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// setupTempLogDir 把日志目录指向 t.TempDir()，避免测试污染真实 %AppData%\ding-ssh\logs，
// 并在测试结束后恢复所有全局状态（注册顺序保证先关文件再恢复目录）。
func setupTempLogDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	SetLogDirForTest(dir)
	t.Cleanup(func() {
		_ = SetFileEnabled(false)
		SetEnabled(false)
		_ = SetAPILogEnabled(false)
		SetLevel("info")
		SetLogDirForTest("")
	})
	return dir
}

// readCurrent 读取日志目录里最新的一个文件内容，用于断言落盘结果。
func readCurrent(t *testing.T, dir string) string {
	t.Helper()
	files, err := ListFiles()
	if err != nil {
		t.Fatalf("列举日志文件失败: %v", err)
	}
	if len(files) == 0 {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, files[0].Name))
	if err != nil {
		t.Fatalf("读取日志文件失败: %v", err)
	}
	return string(data)
}

// captureStdout 临时把 os.Stdout 换成管道，返回「恢复 stdout 并取回内容」的函数。
// 用它验证 stdout 通道，同时避免日志污染 go test 的默认输出。
func captureStdout(t *testing.T) func() string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建管道失败: %v", err)
	}
	os.Stdout = w
	return func() string {
		os.Stdout = old
		_ = w.Close()
		data, _ := io.ReadAll(r)
		_ = r.Close()
		return string(data)
	}
}

// silenceStdout 把 os.Stdout 指向 NUL，返回恢复函数。
// 用于产生大量日志的用例：既能验证落盘，又不会把 go test 输出淹没。
// 不用管道是因为管道缓冲区有限，写入量大时会阻塞。
func silenceStdout(t *testing.T) func() {
	t.Helper()
	old := os.Stdout
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("打开 %s 失败: %v", os.DevNull, err)
	}
	os.Stdout = f
	return func() {
		os.Stdout = old
		_ = f.Close()
	}
}

// assertRedacted 断言输出已打码，且不含任何明文原文。
func assertRedacted(t *testing.T, tag, out string, raws []string) {
	t.Helper()
	if !strings.Contains(out, redacted) {
		t.Fatalf("%s 输出里没有打码占位符: %q", tag, out)
	}
	for _, raw := range raws {
		if strings.Contains(out, raw) {
			t.Fatalf("%s 输出仍含明文 %q: %q", tag, raw, out)
		}
	}
}

// logNames 返回目录里所有 app-*.log 的文件名。
func logNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取日志目录失败: %v", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && isLogFileName(e.Name()) {
			names = append(names, e.Name())
		}
	}
	return names
}

// 开启文件日志后普通日志应同时落盘，开关状态幂等可切换。
func TestFileEnabledWritesToFile(t *testing.T) {
	dir := setupTempLogDir(t)

	if FileEnabled() {
		t.Fatal("默认不应开启文件日志")
	}
	SetEnabled(true)
	if err := SetFileEnabled(true); err != nil {
		t.Fatalf("开启文件日志失败: %v", err)
	}
	if !FileEnabled() {
		t.Fatal("开启后 FileEnabled() 应为 true")
	}
	// 幂等：重复开启不应重开句柄（重复开启会泄漏句柄，导致文件无法删除），也不应报错
	if err := SetFileEnabled(true); err != nil {
		t.Fatalf("重复开启文件日志失败: %v", err)
	}
	startCapture := captureStdout(t)
	Infof("hello %s", "world")
	stdout := startCapture()
	if !strings.Contains(stdout, "[INFO] hello world") {
		t.Fatalf("stdout 内容 = %q，期望包含 \"[INFO] hello world\"", stdout)
	}
	if err := SetFileEnabled(false); err != nil {
		t.Fatalf("关闭文件日志失败: %v", err)
	}
	if err := SetFileEnabled(false); err != nil {
		t.Fatalf("重复关闭文件日志失败: %v", err)
	}
	if FileEnabled() {
		t.Fatal("关闭后 FileEnabled() 应为 false")
	}

	got := readCurrent(t, dir)
	if !strings.Contains(got, "[INFO] hello world") {
		t.Fatalf("日志文件内容 = %q，期望包含 \"[INFO] hello world\"", got)
	}
	// 文件名必须带当天本地日期，便于用户按天定位
	want := logFilePrefix + time.Now().Format(dateLayout) + logFileSuffix
	if names := logNames(t, dir); len(names) != 1 || names[0] != want {
		t.Fatalf("日志文件名 = %v，期望 [%s]", names, want)
	}
}

// 关闭文件日志后不应再落盘，但 stdout 总开关仍然独立生效。
func TestFileDisabledStopsWriting(t *testing.T) {
	dir := setupTempLogDir(t)

	SetEnabled(true)
	if err := SetFileEnabled(true); err != nil {
		t.Fatalf("开启文件日志失败: %v", err)
	}
	Infof("before-off")
	if err := SetFileEnabled(false); err != nil {
		t.Fatalf("关闭文件日志失败: %v", err)
	}
	Infof("after-off")

	if got := readCurrent(t, dir); strings.Contains(got, "after-off") {
		t.Fatalf("关闭文件日志后仍写入: %q", got)
	}
}

// 日志级别过滤：默认 info 级别下 Debugf 不输出，切换 level 后生效，非法取值回落 info。
func TestLevelFiltering(t *testing.T) {
	dir := setupTempLogDir(t)

	if Level() != "info" {
		t.Fatalf("默认级别 = %q，期望 info", Level())
	}
	SetEnabled(true)
	if err := SetFileEnabled(true); err != nil {
		t.Fatalf("开启文件日志失败: %v", err)
	}
	Debugf("debug-line")
	Infof("info-line")
	Warnf("warn-line")
	Errorf("error-line")
	if err := SetFileEnabled(false); err != nil {
		t.Fatalf("关闭文件日志失败: %v", err)
	}

	got := readCurrent(t, dir)
	for _, want := range []string{"[INFO] info-line", "[WARN] warn-line", "[ERROR] error-line"} {
		if !strings.Contains(got, want) {
			t.Fatalf("默认级别下缺少 %q，实际内容 %q", want, got)
		}
	}
	if strings.Contains(got, "debug-line") {
		t.Fatalf("info 级别下不应输出 DEBUG，实际内容 %q", got)
	}

	SetLevel("debug")
	if Level() != "debug" {
		t.Fatalf("SetLevel(\"debug\") 后 Level() = %q", Level())
	}
	if err := SetFileEnabled(true); err != nil {
		t.Fatalf("重新开启文件日志失败: %v", err)
	}
	Debugf("debug-visible")
	SetFileEnabled(false)
	if got := readCurrent(t, dir); !strings.Contains(got, "[DEBUG] debug-visible") {
		t.Fatalf("debug 级别下缺少 DEBUG 输出，实际内容 %q", got)
	}

	// 级别名大小写与首尾空白应被忽略，非法值回落 info
	SetLevel(" WARN ")
	if Level() != "warn" {
		t.Fatalf("SetLevel(\" WARN \") 后 Level() = %q，期望 warn", Level())
	}
	SetLevel("verbose")
	if Level() != "info" {
		t.Fatalf("非法级别后 Level() = %q，期望回落 info", Level())
	}
}

// 调用日志默认关闭，开启后带 [api] 前缀并走同一落盘通道。
func TestAPILogToggle(t *testing.T) {
	dir := setupTempLogDir(t)

	if APILogEnabled() {
		t.Fatal("默认不应开启调用日志")
	}
	if err := SetFileEnabled(true); err != nil {
		t.Fatalf("开启文件日志失败: %v", err)
	}
	APILogf("call %d", 1)
	if got := readCurrent(t, dir); got != "" {
		t.Fatalf("调用日志关闭时不应输出，实际内容 %q", got)
	}

	if err := SetAPILogEnabled(true); err != nil {
		t.Fatalf("开启调用日志失败: %v", err)
	}
	if !APILogEnabled() {
		t.Fatal("开启后 APILogEnabled() 应为 true")
	}
	defer silenceStdout(t)()
	APILogf("call %d", 2)
	_ = SetAPILogEnabled(false)

	got := readCurrent(t, dir)
	if !strings.Contains(got, "[api] call 2") {
		t.Fatalf("调用日志内容 = %q，期望包含 \"[api] call 2\"", got)
	}
	if strings.Contains(got, "call 1") {
		t.Fatalf("调用日志关闭期间的输出不应落盘: %q", got)
	}
}

// ListFiles 应按修改时间倒序返回，TotalSize 应统计全部日志文件。
func TestListFilesAndTotalSize(t *testing.T) {
	dir := setupTempLogDir(t)

	base := time.Now().Add(-24 * time.Hour)
	for i := 0; i < 3; i++ {
		name := filepath.Join(dir, fmt.Sprintf("app-2024010%d.log", i+1))
		if err := os.WriteFile(name, []byte(strings.Repeat("x", (i+1)*10)), 0o644); err != nil {
			t.Fatalf("准备日志文件失败: %v", err)
		}
		ts := base.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(name, ts, ts); err != nil {
			t.Fatalf("设置修改时间失败: %v", err)
		}
	}
	// 不属于本包管理的文件应被忽略
	if err := os.WriteFile(filepath.Join(dir, "other.log"), []byte("no"), 0o644); err != nil {
		t.Fatalf("准备无关文件失败: %v", err)
	}

	files, err := ListFiles()
	if err != nil {
		t.Fatalf("列举日志文件失败: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("文件数 = %d，期望 3", len(files))
	}
	if files[0].Name != "app-20240103.log" || files[2].Name != "app-20240101.log" {
		t.Fatalf("排序结果 = %v，期望按修改时间倒序", files)
	}
	if files[0].Size != 30 {
		t.Fatalf("最新文件大小 = %d，期望 30", files[0].Size)
	}
	if files[0].ModTime < files[1].ModTime || files[1].ModTime < files[2].ModTime {
		t.Fatalf("ModTime 未按倒序返回: %v", files)
	}

	total, err := TotalSize()
	if err != nil {
		t.Fatalf("统计总大小失败: %v", err)
	}
	if total != 60 {
		t.Fatalf("总大小 = %d，期望 60（10+20+30）", total)
	}
}

// Clear 应删除全部日志文件、返回删除数量，并在文件日志仍开启时继续可写。
func TestClearRemovesFilesAndReopens(t *testing.T) {
	dir := setupTempLogDir(t)

	SetEnabled(true)
	if err := SetFileEnabled(true); err != nil {
		t.Fatalf("开启文件日志失败: %v", err)
	}
	Infof("before-clear")

	n, err := Clear()
	if err != nil {
		t.Fatalf("Clear 失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("Clear 删除数量 = %d，期望 1", n)
	}
	// 清理后重新打开的新文件应存在且为空
	if names := logNames(t, dir); len(names) != 1 {
		t.Fatalf("Clear 后文件列表 = %v，期望仅保留重新打开的空文件", names)
	}
	if got := readCurrent(t, dir); got != "" {
		t.Fatalf("Clear 后新文件应为空，实际内容 %q", got)
	}

	Infof("after-clear")
	if got := readCurrent(t, dir); !strings.Contains(got, "after-clear") {
		t.Fatalf("Clear 后应继续写入，实际内容 %q", got)
	}
	if n, err := Clear(); err != nil || n != 1 {
		t.Fatalf("第二次 Clear = (%d, %v)，期望 (1, nil)", n, err)
	}
	_ = SetFileEnabled(false)
}

// 目录不存在时 Clear/ListFiles/TotalSize/ReadTail 都应返回零值而不是错误。
func TestMissingDirIsNotAnError(t *testing.T) {
	SetLogDirForTest(filepath.Join(t.TempDir(), "not-exist"))
	t.Cleanup(func() {
		_ = SetFileEnabled(false)
		SetLogDirForTest("")
	})

	if files, err := ListFiles(); err != nil || len(files) != 0 {
		t.Fatalf("ListFiles = (%v, %v)，期望空列表与 nil", files, err)
	}
	if total, err := TotalSize(); err != nil || total != 0 {
		t.Fatalf("TotalSize = (%d, %v)，期望 0 与 nil", total, err)
	}
	if n, err := Clear(); err != nil || n != 0 {
		t.Fatalf("Clear = (%d, %v)，期望 (0, nil)", n, err)
	}
	if tail, err := ReadTail(10); err != nil || tail != "" {
		t.Fatalf("ReadTail = (%q, %v)，期望空串与 nil", tail, err)
	}
}

// ReadTail 应只返回文件末尾若干行，且在文件很大时不全量读取。
func TestReadTail(t *testing.T) {
	dir := setupTempLogDir(t)

	// 只验证落盘，因此关掉控制台，避免测试输出被 500 行日志淹没
	SetEnabled(false)
	if err := SetFileEnabled(true); err != nil {
		t.Fatalf("开启文件日志失败: %v", err)
	}
	for i := 1; i <= 500; i++ {
		Infof("line-%03d", i)
	}
	if err := SetFileEnabled(false); err != nil {
		t.Fatalf("关闭文件日志失败: %v", err)
	}

	tail, err := ReadTail(10)
	if err != nil {
		t.Fatalf("ReadTail 失败: %v", err)
	}
	lines := strings.Split(tail, "\n")
	if len(lines) != 10 {
		t.Fatalf("ReadTail(10) 行数 = %d，期望 10\n内容: %q", len(lines), tail)
	}
	if !strings.HasSuffix(lines[9], "line-500") {
		t.Fatalf("最后一行 = %q，期望以 line-500 结尾", lines[9])
	}
	if !strings.Contains(lines[0], "line-491") {
		t.Fatalf("第 1 行 = %q，期望包含 line-491", lines[0])
	}

	// lines <= 0 时按默认 200 行处理
	tail, err = ReadTail(0)
	if err != nil {
		t.Fatalf("ReadTail(0) 失败: %v", err)
	}
	if got := len(strings.Split(tail, "\n")); got != defaultTailLines {
		t.Fatalf("ReadTail(0) 行数 = %d，期望 %d", got, defaultTailLines)
	}

	// 请求行数超过文件总行数时返回全部内容
	tail, err = ReadTail(10000)
	if err != nil {
		t.Fatalf("ReadTail(10000) 失败: %v", err)
	}
	if got := len(strings.Split(tail, "\n")); got != 500 {
		t.Fatalf("ReadTail(10000) 行数 = %d，期望 500", got)
	}
	if !strings.Contains(tail, "line-001") {
		t.Fatal("ReadTail(10000) 应包含最早的一行")
	}

	// 文件内容与直接读取一致（抽查末尾）
	if !strings.HasSuffix(strings.TrimRight(readCurrent(t, dir), "\n"), "line-500") {
		t.Fatal("ReadTail 末尾行与文件内容不一致")
	}
}

// 超过阈值应轮转，且目录内文件数不超过 maxFileCount（保留最新）。
func TestRotateAndPrune(t *testing.T) {
	dir := setupTempLogDir(t)

	SetEnabled(true)
	if err := SetFileEnabled(true); err != nil {
		t.Fatalf("开启文件日志失败: %v", err)
	}
	Infof("first")

	// 预置若干更旧的历史文件，验证轮转后会裁掉最旧的
	old := time.Now().Add(-72 * time.Hour)
	for i := 0; i < maxFileCount+4; i++ {
		name := filepath.Join(dir, fmt.Sprintf("app-202301%02d.log", i+1))
		if err := os.WriteFile(name, []byte("old\n"), 0o644); err != nil {
			t.Fatalf("准备旧日志失败: %v", err)
		}
		ts := old.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(name, ts, ts); err != nil {
			t.Fatalf("设置修改时间失败: %v", err)
		}
	}

	// 直接把已写字节数推到阈值，等价于单文件写满 8MB，避免测试真的写 8MB 数据
	fileMu.Lock()
	fileSize = maxFileSize
	fileMu.Unlock()
	Infof("after-rotate")

	if err := SetFileEnabled(false); err != nil {
		t.Fatalf("关闭文件日志失败: %v", err)
	}

	files, err := ListFiles()
	if err != nil {
		t.Fatalf("列举日志文件失败: %v", err)
	}
	if len(files) > maxFileCount {
		t.Fatalf("轮转后文件数 = %d，期望不超过 %d", len(files), maxFileCount)
	}
	rotated := 0
	for _, f := range files {
		if strings.Contains(f.Name, "-") {
			rotated++
		}
	}
	if rotated == 0 {
		t.Fatalf("未找到轮转文件，文件列表 = %v", files)
	}
	// 轮转后当前文件只包含轮转之后写入的内容
	got := readCurrent(t, dir)
	if !strings.Contains(got, "after-rotate") || strings.Contains(got, "first") {
		t.Fatalf("轮转后当前文件内容 = %q，期望只有 after-rotate", got)
	}
}

// Close 应关闭句柄并关闭开关，重复调用不报错。
func TestClose(t *testing.T) {
	dir := setupTempLogDir(t)

	SetEnabled(true)
	if err := SetFileEnabled(true); err != nil {
		t.Fatalf("开启文件日志失败: %v", err)
	}
	if err := Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	if FileEnabled() {
		t.Fatal("Close 后 FileEnabled() 应为 false")
	}
	if err := Close(); err != nil {
		t.Fatalf("重复 Close 失败: %v", err)
	}
	Infof("after-close")
	if got := readCurrent(t, dir); strings.Contains(got, "after-close") {
		t.Fatalf("Close 后不应再写入文件: %q", got)
	}
}

// 两路通道互相独立（设置页上是两个并列开关）：关掉控制台后文件照常落盘。
func TestFileChannelIndependentOfConsole(t *testing.T) {
	dir := setupTempLogDir(t)

	SetEnabled(false)
	if err := SetFileEnabled(true); err != nil {
		t.Fatalf("开启文件日志失败: %v", err)
	}
	stop := captureStdout(t)
	Infof("file-only")
	stdout := stop()
	if stdout != "" {
		t.Fatalf("控制台已关闭，不应有任何输出，实际 %q", stdout)
	}
	if got := readCurrent(t, dir); !strings.Contains(got, "[INFO] file-only") {
		t.Fatalf("文件日志开启时应落盘，实际内容 %q", got)
	}
}

// 只开控制台日志时不得落盘，连文件都不应创建。
func TestConsoleChannelIndependentOfFile(t *testing.T) {
	dir := setupTempLogDir(t)

	SetEnabled(true)
	if err := SetFileEnabled(false); err != nil {
		t.Fatalf("关闭文件日志失败: %v", err)
	}
	stop := captureStdout(t)
	Infof("console-only")
	stdout := stop()
	if !strings.Contains(stdout, "[INFO] console-only") {
		t.Fatalf("控制台开启时应输出，实际 %q", stdout)
	}
	if names := logNames(t, dir); len(names) != 0 {
		t.Fatalf("文件日志关闭时不应创建日志文件，实际 %v", names)
	}
	if got := readCurrent(t, dir); got != "" {
		t.Fatalf("文件日志关闭时不应落盘，实际内容 %q", got)
	}
}

// 两路都关 = 完全不留痕；APILogf 例外，它只受自己的开关控制。
func TestBothChannelsDisabled(t *testing.T) {
	dir := setupTempLogDir(t)

	SetLevel("debug") // 排除级别过滤的干扰，确保「无输出」确实来自通道关闭
	SetEnabled(false)
	if err := SetFileEnabled(false); err != nil {
		t.Fatalf("关闭文件日志失败: %v", err)
	}
	stop := captureStdout(t)
	Debugf("no-channel-debug")
	Infof("no-channel-info")
	Warnf("no-channel-warn")
	Errorf("no-channel-error")
	stdout := stop()
	if stdout != "" {
		t.Fatalf("两路都关时控制台不应有输出，实际 %q", stdout)
	}
	if names := logNames(t, dir); len(names) != 0 {
		t.Fatalf("两路都关时不应创建日志文件，实际 %v", names)
	}
	if got := readCurrent(t, dir); got != "" {
		t.Fatalf("两路都关时不应落盘，实际内容 %q", got)
	}

	// APILogf 独立于总开关与文件开关：自己的开关打开就输出
	if err := SetAPILogEnabled(true); err != nil {
		t.Fatalf("开启调用日志失败: %v", err)
	}
	stop = captureStdout(t)
	APILogf("api-independent")
	stdout = stop()
	if !strings.Contains(stdout, "[api] api-independent") {
		t.Fatalf("调用日志开关打开时应输出，实际 %q", stdout)
	}
	if got := readCurrent(t, dir); got != "" {
		t.Fatalf("文件通道关闭时调用日志不应落盘，实际内容 %q", got)
	}
	_ = SetAPILogEnabled(false)
}

// 并发写入与管理调用（列举/统计/读尾）不应丢日志、panic 或死锁。
func TestConcurrentWriteAndManage(t *testing.T) {
	dir := setupTempLogDir(t)

	// 只验证落盘与并发安全，控制台关闭即可（文件通道独立生效）
	SetEnabled(false)
	if err := SetFileEnabled(true); err != nil {
		t.Fatalf("开启文件日志失败: %v", err)
	}

	const (
		writers = 8
		perOne  = 200
	)
	var wg sync.WaitGroup
	for id := 0; id < writers; id++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < perOne; j++ {
				Infof("g%d-%d", id, j)
			}
		}(id)
	}
	for i := 0; i < 20; i++ {
		if _, err := ListFiles(); err != nil {
			t.Errorf("并发 ListFiles 失败: %v", err)
		}
		if _, err := TotalSize(); err != nil {
			t.Errorf("并发 TotalSize 失败: %v", err)
		}
		if _, err := ReadTail(50); err != nil {
			t.Errorf("并发 ReadTail 失败: %v", err)
		}
	}
	wg.Wait()
	_ = SetFileEnabled(false)

	got := readCurrent(t, dir)
	for _, id := range []int{0, writers - 1} {
		tag := fmt.Sprintf("g%d-", id)
		if n := strings.Count(got, tag); n != perOne {
			t.Fatalf("并发写入后 %s 出现 %d 次，期望 %d 次（存在丢写/覆盖）", tag, n, perOne)
		}
	}
	if n := strings.Count(got, "[INFO] g"); n != writers*perOne {
		t.Fatalf("并发写入后总行数 = %d，期望 %d", n, writers*perOne)
	}
}

// LogDir 必须落在 os.UserConfigDir()/ding-ssh/logs，与 internal/store 的布局一致。
// 该用例只做路径断言，不写真实目录。
func TestLogDirFollowsUserConfigDir(t *testing.T) {
	SetLogDirForTest("")
	t.Cleanup(func() { SetLogDirForTest("") })

	cfg, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("当前环境无法获取用户配置目录: %v", err)
	}
	want := filepath.Join(cfg, "ding-ssh", "logs")
	if got := LogDir(); got != want {
		t.Fatalf("LogDir() = %q，期望 %q", got, want)
	}
}

// 输出管道脱敏：四个级别、两路通道写出的 token / JSON 密码 / 私钥都要打码。
// 用户要求「一律打码、默认开、不可关闭」，因此这属于 logx 自身职责，
// 不能只依赖 APILogf 的调用方。
func TestOutputRedactsCredentialsOnBothChannels(t *testing.T) {
	dir := setupTempLogDir(t)

	SetLevel("debug") // 让 Debugf 也参与
	payloads := []string{
		"token=abcdef123456",
		`"password":"hunter2"`,
		"-----BEGIN OPENSSH PRIVATE KEY-----",
	}
	raws := []string{"abcdef123456", "hunter2", "PRIVATE KEY"}
	levels := []struct {
		name string
		fn   func(string, ...interface{})
	}{
		{"Debugf", Debugf},
		{"Infof", Infof},
		{"Warnf", Warnf},
		{"Errorf", Errorf},
	}
	// fire 用四个级别各写一遍全部敏感样例，返回写入的行数。
	fire := func(tag string) int {
		n := 0
		for _, lv := range levels {
			for _, p := range payloads {
				lv.fn("%s %s %s", tag, lv.name, p)
				n++
			}
		}
		return n
	}

	// 文件通道：控制台关闭也要落盘，且落盘内容已脱敏
	SetEnabled(false)
	if err := SetFileEnabled(true); err != nil {
		t.Fatalf("开启文件日志失败: %v", err)
	}
	wantLines := fire("file")
	if err := SetFileEnabled(false); err != nil {
		t.Fatalf("关闭文件日志失败: %v", err)
	}
	got := readCurrent(t, dir)
	assertRedacted(t, "文件通道", got, raws)
	// 脱敏不得破坏行结构（私钥正则会吞掉行尾换行，redactLine 负责补回）
	if n := len(strings.Split(strings.TrimRight(got, "\n"), "\n")); n != wantLines {
		t.Fatalf("文件通道行数 = %d，期望 %d（脱敏破坏了行结构）\n内容: %q", n, wantLines, got)
	}

	// 控制台通道：文件关闭，输出同样已脱敏
	SetEnabled(true)
	stop := captureStdout(t)
	fire("console")
	stdout := stop()
	assertRedacted(t, "控制台通道", stdout, raws)
}

// 已脱敏文本再次 Redact 必须不变：调用方先脱敏 + 输出管道再脱敏不得出现 *** 套娃。
func TestRedactIdempotent(t *testing.T) {
	for _, in := range []string{
		"token=abcdef123456",
		`{"password":"hunter2","keyContent":"-----BEGINx","port":22}`,
		"Authorization: Bearer 9f8e7d6c5b4a status=401",
		"-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----",
		"token: 8f3c2b&access_token=zzz-999",
	} {
		once := Redact(in)
		if !strings.Contains(once, redacted) {
			t.Fatalf("首次脱敏未命中: in=%q once=%q", in, once)
		}
		if twice := Redact(once); twice != once {
			t.Fatalf("Redact 不幂等: in=%q once=%q twice=%q", in, once, twice)
		}
		if strings.Contains(once, redacted+redacted) {
			t.Fatalf("出现 *** 套娃: in=%q once=%q", in, once)
		}
	}
}

// 快速路径：不含任何敏感标记的普通文本原样返回，不进入正则慢路径。
func TestRedactFastPathPlainText(t *testing.T) {
	for _, s := range []string{
		"hello world",
		"[INFO] SSH 连接成功: session=1 server=web-01 host=10.0.0.1:22",
		"终端输出：total 12 files",
		strings.Repeat("terminal output line; files=12 dirs=3; ", 200),
	} {
		if hasSensitiveMarker(s) {
			t.Fatalf("普通文本被误判为含敏感标记: %q", s)
		}
		if got := Redact(s); got != s {
			t.Fatalf("快速路径未原样返回: Redact(%q) = %q", s, got)
		}
	}
}

// 第三方库经标准库 log 的输出（stdLogWriter 路径）同样必须落盘前脱敏，
// 否则 net/http 之类会把带 token 的 URL 原样写进日志文件。
func TestStdLogWriterRedacts(t *testing.T) {
	dir := setupTempLogDir(t)

	SetEnabled(false)
	if err := SetFileEnabled(true); err != nil {
		t.Fatalf("开启文件日志失败: %v", err)
	}
	log.Println("GET /v1/state?token=abcdef123456&limit=10 200")
	log.Printf("Authorization: Bearer 9f8e7d6c5b4a\n")
	if err := SetFileEnabled(false); err != nil {
		t.Fatalf("关闭文件日志失败: %v", err)
	}

	got := readCurrent(t, dir)
	assertRedacted(t, "stdLogWriter", got, []string{"abcdef123456", "9f8e7d6c5b4a"})
	if !strings.Contains(got, "token="+redacted) {
		t.Fatalf("查询串 token 未被脱敏: %q", got)
	}
}

// BenchmarkRedactPlain 给出快速路径（无敏感标记）的每行开销，热路径逐行落盘时最关键。
func BenchmarkRedactPlain(b *testing.B) {
	line := "[INFO] SSH 连接成功: session=1 server=web-01 host=10.0.0.1:22"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Redact(line)
	}
}

// BenchmarkRedactSecret 作为对照：命中标记、真正跑正则的慢路径开销。
func BenchmarkRedactSecret(b *testing.B) {
	line := "[api] GET /v1/state?token=abcdef123456&limit=10 200 3ms"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Redact(line)
	}
}

// BenchmarkRedactPlainChinese 对照含中文的日志行：ToLower 会分配一次内存，
// 但字符串搜索本身走 SIMD，整体仍比慢路径便宜一个数量级。
func BenchmarkRedactPlainChinese(b *testing.B) {
	line := "[INFO] SSH 连接成功: session=1 server=web-01 host=10.0.0.1:22"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Redact(line)
	}
}
