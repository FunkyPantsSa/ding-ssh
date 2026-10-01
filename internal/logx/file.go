// 文件日志：把日志同时落盘到用户配置目录，供「日志」设置页查看与清理。
//
// 目录与文件名约定（与 internal/store 的 os.UserConfigDir()/ding-ssh 布局一致，
// 让用户在同一处找到应用数据）：
//
//	%AppData%\ding-ssh\logs\app-YYYYMMDD.log          当前写入文件（本地日期）
//	%AppData%\ding-ssh\logs\app-YYYYMMDD-HHMMSS.log   轮转后的历史文件
//
// 单文件超过 8MB 或跨天时轮转，目录内最多保留 20 个文件，超出删最旧。
package logx

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// logFilePrefix/logFileSuffix 定义日志文件名。ListFiles/Clear/裁剪都按它筛选，
	// 集中在这里可以避免多处硬编码后互相失配（例如设置页列表里冒出无关文件）。
	logFilePrefix = "app-"
	logFileSuffix = ".log"
	// maxFileSize 单文件轮转阈值 8MB：既便于排查，又避免单文件过大拖慢日志页加载。
	maxFileSize int64 = 8 << 20
	// maxFileCount 日志目录保留的最大文件数（含当前文件），超出删最旧，防止磁盘被日志吃满。
	maxFileCount = 20
	// dateLayout 当前日志文件名里的日期格式（本地时区）。
	dateLayout = "20060102"
	// rotateTimeLayout 轮转文件名里的时间戳格式，精确到秒足以区分。
	rotateTimeLayout = "20060102-150405"
	// defaultTailLines ReadTail 未指定行数时的默认值。
	defaultTailLines = 200
	// readTailBlockSize ReadTail 从文件尾往前读取的块大小。
	readTailBlockSize = 8 << 10
	// readTailMaxBytes ReadTail 最多回读的字节数：防止某行超长（例如贴了整段
	// 终端输出）时把整个文件读进内存。达到上限就返回已读到的内容。
	readTailMaxBytes = 4 << 20
	// openRetryInterval 打开日志文件失败后的重试间隔：避免每条日志都做一次
	// 注定失败的系统调用（目录被删/无权限时会持续失败）。
	openRetryInterval = 5 * time.Second
)

// LogFile 日志文件元信息，供设置页列表展示。
type LogFile struct {
	Name    string // 文件名（不含目录）
	Size    int64  // 字节数
	ModTime int64  // 最后修改时间，毫秒时间戳（前端 Date 可直接用）
}

// logDirOverride 允许测试把日志目录指向 t.TempDir()，避免污染真实 %AppData%。
// 用 atomic.Pointer 是因为它可能被测试 goroutine 与日志写入 goroutine 同时读取。
var logDirOverride atomic.Pointer[string]

var (
	// fileMu 保护下面这组文件句柄状态。日志写入频繁，用一把互斥锁把
	// 「判轮转 + 写文件」做成原子操作，避免并发写入互相覆盖。
	fileMu sync.Mutex
	// fileEnabled 文件日志开关。
	fileEnabled atomic.Bool
	// fileHandle 当前打开的日志文件句柄，nil 表示尚未打开/已被关闭。
	fileHandle *os.File
	// filePath 当前句柄对应的完整路径，裁剪旧文件时用它确保不删正在写的文件。
	filePath string
	// fileSize 当前文件已写字节数。用它判断轮转，省去每条日志一次 Stat 系统调用。
	fileSize int64
	// fileDate 当前文件对应的本地日期，与 now 不同说明跨天，需要换新文件。
	fileDate string
	// fileOpenFailedAt 上次打开失败的时间，用于失败退避。
	fileOpenFailedAt time.Time
)

// LogDir 返回日志目录：%AppData%\ding-ssh\logs\（Windows 下即 os.UserConfigDir()）。
// 获取用户配置目录失败时退化为相对路径 logs，保证调用方不必处理错误。
func LogDir() string {
	if p := logDirOverride.Load(); p != nil && *p != "" {
		return *p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "logs"
	}
	return filepath.Join(dir, "ding-ssh", "logs")
}

// SetLogDirForTest 覆盖日志目录，仅供测试使用（传空串恢复默认）。
// 真实运行路径固定在 %AppData%\ding-ssh\logs，不对外开放配置，
// 避免设置页乱填路径后日志写到不可预期的位置。
func SetLogDirForTest(dir string) {
	if dir == "" {
		logDirOverride.Store(nil)
		return
	}
	logDirOverride.Store(&dir)
}

// SetFileEnabled 开关文件日志，幂等。
//
// 开启：确保目录存在并打开当天的日志文件（追加写入），失败时保持关闭并返回错误，
// 让设置页能提示用户，同时控制台输出完全不受影响。
// 关闭：关闭句柄，重复关闭不报错。
//
// 注意：文件日志与控制台日志（SetEnabled）是两个并列的独立开关：
// 关闭控制台后本开关仍然生效，日志照常落盘；只有两者都关才完全不留痕。
func SetFileEnabled(v bool) error {
	fileMu.Lock()
	defer fileMu.Unlock()
	if !v {
		fileEnabled.Store(false)
		return closeHandleLocked()
	}
	now := time.Now()
	// 幂等：句柄已指向当天文件时直接返回。重复开启必须先判断，
	// 否则 openLocked 会覆盖旧句柄导致句柄泄漏（Windows 下文件从此无法改名/删除）。
	if fileHandle != nil && fileDate == now.Format(dateLayout) {
		fileEnabled.Store(true)
		return nil
	}
	// 跨天或句柄异常时先关掉旧句柄再重开。
	_ = closeHandleLocked()
	if err := openLocked(now); err != nil {
		fileEnabled.Store(false)
		return err
	}
	fileEnabled.Store(true)
	return nil
}

// FileEnabled 返回文件日志开关状态。
func FileEnabled() bool {
	return fileEnabled.Load()
}

// Close 关闭日志文件句柄并关闭文件日志开关，应用退出时调用。
// 与 SetFileEnabled(false) 等价，单独提供是为了让退出流程的意图更清晰。
func Close() error {
	fileMu.Lock()
	defer fileMu.Unlock()
	fileEnabled.Store(false)
	return closeHandleLocked()
}

// ListFiles 返回日志目录内的全部 app-*.log，按最后修改时间倒序（最新在前）。
// 目录不存在（用户从未开过文件日志）时返回空切片与 nil，而不是错误：
// 设置页首次打开日志页不应该看到一个红色报错。
func ListFiles() ([]LogFile, error) {
	fileMu.Lock()
	defer fileMu.Unlock()
	return listFiles()
}

// TotalSize 返回日志目录内所有 app-*.log 的字节数总和（目录不存在时为 0）。
func TotalSize() (int64, error) {
	fileMu.Lock()
	defer fileMu.Unlock()
	files, err := listFiles()
	if err != nil {
		return 0, err
	}
	var total int64
	for _, f := range files {
		total += f.Size
	}
	return total, nil
}

// Clear 清空日志目录内的全部 app-*.log 文件，返回实际删除的文件数与错误。
//
// 流程：先关闭当前句柄（Windows 下删除被占用的文件会失败），再逐个删除，
// 若清理前文件日志处于开启状态则重新打开一个新文件，保证调用后日志继续正常写入。
// 单个文件删除失败不会中断其余文件的清理，错误返回第一个失败原因；
// 目录不存在视为已经清空，返回 0 与 nil。
func Clear() (int, error) {
	fileMu.Lock()
	defer fileMu.Unlock()

	keepEnabled := fileEnabled.Load()
	// 关闭失败（句柄已失效等）不影响后续删除，忽略错误继续。
	_ = closeHandleLocked()

	dir := LogDir()
	removed := 0
	var firstErr error
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			firstErr = err
		}
	} else {
		for _, e := range entries {
			if e.IsDir() || !isLogFileName(e.Name()) {
				continue
			}
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			removed++
		}
	}

	if keepEnabled {
		if err := openLocked(time.Now()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return removed, firstErr
}

// ReadTail 返回当前（最新）日志文件的最后 lines 行；lines <= 0 时按 200 行处理。
//
// 为避免大文件全量读入内存，从文件尾往前按块读取，最多回读 4MB（见 readTailMaxBytes），
// 因此超长行场景下可能少于请求行数。日志目录为空时返回空字符串与 nil。
func ReadTail(lines int) (string, error) {
	if lines <= 0 {
		lines = defaultTailLines
	}
	files, err := ListFiles()
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", nil
	}
	return readTailFile(filepath.Join(LogDir(), files[0].Name), lines)
}

// writeFile 把一行日志追加到文件。
//
// 任何失败都只体现为「文件里少了一行」：不 panic、不向上报错、更不影响 stdout。
// 日志本身出问题不应该再制造新的错误处理分支（否则容易递归）。
func writeFile(line string) {
	if !fileEnabled.Load() {
		return
	}
	fileMu.Lock()
	defer fileMu.Unlock()

	now := time.Now()
	if fileHandle == nil {
		// 打开失败后退避一段时间再试，避免磁盘满/目录被删时每条日志都打一次系统调用。
		if !fileOpenFailedAt.IsZero() && now.Sub(fileOpenFailedAt) < openRetryInterval {
			return
		}
		if err := openLocked(now); err != nil {
			return
		}
	}
	// 跨天或超过阈值都换新文件，保证文件名里的日期始终等于写入日期。
	if fileDate != now.Format(dateLayout) || fileSize >= maxFileSize {
		if err := rotateLocked(now); err != nil {
			// 轮转失败（例如文件被外部程序占用）就继续写原文件：
			// 丢日志比中断写入更糟，且下次写入还会再尝试轮转。
			if fileHandle == nil {
				return
			}
		}
	}
	if fileHandle == nil {
		return
	}
	n, _ := fileHandle.WriteString(line)
	fileSize += int64(n)
}

// openLocked 打开当天的日志文件（追加写），必要时先创建目录。
// 调用者必须持有 fileMu。
func openLocked(now time.Time) error {
	date := now.Format(dateLayout)
	dir := LogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fileOpenFailedAt = now
		return fmt.Errorf("创建日志目录失败: %w", err)
	}
	path := filepath.Join(dir, logFilePrefix+date+logFileSuffix)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fileOpenFailedAt = now
		return fmt.Errorf("打开日志文件失败: %w", err)
	}
	fileHandle = f
	filePath = path
	fileDate = date
	fileOpenFailedAt = time.Time{}
	if info, err := f.Stat(); err == nil {
		fileSize = info.Size()
	} else {
		fileSize = 0
	}
	return nil
}

// closeHandleLocked 关闭当前句柄并清空状态；没有句柄时直接返回 nil。
// 调用者必须持有 fileMu。
func closeHandleLocked() error {
	if fileHandle == nil {
		return nil
	}
	err := fileHandle.Close()
	fileHandle = nil
	filePath = ""
	fileDate = ""
	fileSize = 0
	return err
}

// rotateLocked 轮转当前日志文件：把 app-YYYYMMDD.log 改名为 app-YYYYMMDD-HHMMSS.log，
// 再新建同名当天文件（跨天时用新日期），最后按数量裁剪旧文件。
// 调用者必须持有 fileMu。改名失败时回退到原文件继续追加，绝不丢日志。
func rotateLocked(now time.Time) error {
	dir := LogDir()
	date := fileDate
	if date == "" {
		date = now.Format(dateLayout)
	}
	rotatedName := uniqueRotatedName(dir, date, now)
	// 先关句柄，否则 Windows 下改名会因文件被占用而失败。
	_ = closeHandleLocked()
	if err := os.Rename(filepath.Join(dir, logFilePrefix+date+logFileSuffix),
		filepath.Join(dir, rotatedName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		// 改名失败：重新打开原文件继续写，本次不裁剪。
		if reopenErr := openLocked(now); reopenErr != nil {
			return reopenErr
		}
		return err
	}
	if err := openLocked(now); err != nil {
		return err
	}
	pruneLocked()
	return nil
}

// uniqueRotatedName 生成不冲突的轮转文件名：同一秒内多次轮转时追加序号后缀，
// 因为 os.Rename 在 Windows 上覆盖已存在文件会失败。
func uniqueRotatedName(dir, date string, now time.Time) string {
	base := logFilePrefix + date + "-" + now.Format(rotateTimeLayout)
	name := base + logFileSuffix
	for i := 1; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, name)); errors.Is(err, os.ErrNotExist) {
			return name
		}
		name = fmt.Sprintf("%s-%d%s", base, i, logFileSuffix)
	}
}

// pruneLocked 只保留最近的 maxFileCount 个日志文件，删除更旧的。
// 调用者必须持有 fileMu；当前正在写入的文件永不删除。
func pruneLocked() {
	files, err := listFiles()
	if err != nil || len(files) <= maxFileCount {
		return
	}
	// 已按修改时间倒序，尾部就是最旧的。
	for _, f := range files[maxFileCount:] {
		path := filepath.Join(LogDir(), f.Name)
		if path == filePath {
			continue
		}
		_ = os.Remove(path)
	}
}

// listFiles 扫描日志目录并按修改时间倒序返回日志文件。
// 调用者必须自行保证 fileMu 语义（本函数不加锁）。
func listFiles() ([]LogFile, error) {
	entries, err := os.ReadDir(LogDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []LogFile{}, nil
		}
		return nil, err
	}
	out := make([]LogFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !isLogFileName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			// 扫描期间文件被外部删除/占用，跳过即可，不影响其余文件。
			continue
		}
		out = append(out, LogFile{
			Name:    e.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime().UnixMilli(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ModTime != out[j].ModTime {
			return out[i].ModTime > out[j].ModTime
		}
		// 同一毫秒（例如刚轮转完）按文件名倒序，保证顺序稳定。
		return out[i].Name > out[j].Name
	})
	return out, nil
}

// isLogFileName 判断文件名是否属于本包管理的日志文件。
func isLogFileName(name string) bool {
	return len(name) > len(logFilePrefix)+len(logFileSuffix) &&
		strings.HasPrefix(name, logFilePrefix) &&
		strings.HasSuffix(name, logFileSuffix)
}

// readTailFile 从文件尾往前分块读取最后 lines 行。
func readTailFile(path string, lines int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := info.Size()
	if size == 0 {
		return "", nil
	}

	// 倒序收集块（尾部块在前），最后再反向拼回原文。
	var (
		chunks   [][]byte
		read     int64
		newlines int
	)
	for offset := size; offset > 0 && newlines <= lines && read < readTailMaxBytes; {
		n := int64(readTailBlockSize)
		if offset < n {
			n = offset
		}
		if read+n > readTailMaxBytes {
			n = readTailMaxBytes - read
		}
		if n <= 0 {
			break
		}
		offset -= n
		buf := make([]byte, n)
		// ReadAt 只在越过文件尾时返回 io.EOF，此时 buf 里的内容依然有效。
		if _, err := f.ReadAt(buf, offset); err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		chunks = append(chunks, buf)
		read += n
		newlines += bytes.Count(buf, newlineBytes)
	}

	var sb strings.Builder
	sb.Grow(int(read))
	for i := len(chunks) - 1; i >= 0; i-- {
		sb.Write(chunks[i])
	}
	return lastLines(sb.String(), lines), nil
}

// newlineBytes 供 bytes.Count 统计换行数，避免每次调用都分配。
var newlineBytes = []byte{'\n'}

// lastLines 返回文本的最后 n 行（不含结尾换行造成的空行）。
// 回读可能从行中间开始，因此首行可能是半行，这里不做处理：
// 日志行都很短，4MB 的回读上限保证首行几乎总是完整的。
func lastLines(text string, n int) string {
	text = strings.TrimRight(text, "\r\n")
	if text == "" {
		return ""
	}
	parts := strings.Split(text, "\n")
	if len(parts) > n {
		parts = parts[len(parts)-n:]
	}
	return strings.Join(parts, "\n")
}
