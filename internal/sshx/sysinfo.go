package sshx

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ding-ssh/internal/models"

	"golang.org/x/crypto/ssh"
)

const (
	sysInfoIntervalActive = 3 * time.Second
	sysInfoIntervalIdle   = 10 * time.Second
	sysInfoCollectTimeout = 8 * time.Second
	sysInfoMaxAttempts    = 2
)

// 明确用 sh -c 执行；段落失败不影响后续（各命令独立 2>/dev/null）。
// 不使用 head，避免 SIGPIPE 在个别 shell/pipefail 下中断整段脚本。
const sysInfoScript = `
echo '---CPU---'
LC_ALL=C cat /proc/stat 2>/dev/null
echo '---MEM---'
LC_ALL=C cat /proc/meminfo 2>/dev/null
echo '---DISK---'
LC_ALL=C df -k -P 2>/dev/null
echo '---NET---'
LC_ALL=C cat /proc/net/dev 2>/dev/null
echo '---IP---'
LC_ALL=C ip -o -4 addr show 2>/dev/null || LC_ALL=C ip -4 -o addr 2>/dev/null || true
echo '---UP---'
LC_ALL=C cat /proc/uptime 2>/dev/null
`

type netSample struct {
	rx, tx uint64
	at     time.Time
}

type cpuSample struct {
	total, idle uint64
}

// SysInfoCollector 在独立无 PTY Session 上周期性采集系统指标。
type SysInfoCollector struct {
	sessionID string
	client    *ssh.Client
	onSnap    func(models.SysInfoSnapshot)
	onError   func(string)

	mu       sync.Mutex
	running  bool
	stopFn   context.CancelFunc
	wg       sync.WaitGroup
	idle     bool
	prevNet  map[string]netSample
	prevCPU  *cpuSample
	lastGood *models.SysInfoSnapshot // 最近一次有效快照，供短暂失败时保留展示
}

func newSysInfoCollector(sessionID string, client *ssh.Client, onSnap func(models.SysInfoSnapshot), onError func(string)) *SysInfoCollector {
	return &SysInfoCollector{
		sessionID: sessionID,
		client:    client,
		onSnap:    onSnap,
		onError:   onError,
		prevNet:   make(map[string]netSample),
	}
}

func (c *SysInfoCollector) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.stopFn = cancel
	c.running = true
	c.wg.Add(1)
	go c.loop(ctx)
	return nil
}

func (c *SysInfoCollector) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	if c.stopFn != nil {
		c.stopFn()
	}
	c.mu.Unlock()
	c.wg.Wait()
}

func (c *SysInfoCollector) SetIdle(idle bool) {
	c.mu.Lock()
	c.idle = idle
	c.mu.Unlock()
}

func (c *SysInfoCollector) loop(ctx context.Context) {
	defer c.wg.Done()
	c.collectOnce()
	for {
		c.mu.Lock()
		idle := c.idle
		c.mu.Unlock()
		interval := sysInfoIntervalActive
		if idle {
			interval = sysInfoIntervalIdle
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
			c.collectOnce()
		}
	}
}

func (c *SysInfoCollector) collectOnce() {
	var raw string
	var lastErr error
	for attempt := 0; attempt < sysInfoMaxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(250 * time.Millisecond)
		}
		out, err := c.runRemoteScript()
		if strings.TrimSpace(out) != "" {
			raw = out
			lastErr = err // 非零退出但有 stdout 时仍解析
			break
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("远程无输出")
		}
	}

	if strings.TrimSpace(raw) == "" {
		msg := "无法采集指标（远程无输出，可能非 Linux）"
		if lastErr != nil {
			msg = fmt.Sprintf("无法采集指标: %v", lastErr)
		}
		c.emitSoftError(msg)
		return
	}

	snap, cpuRaw, counters, ips := parseSysInfoLoose(raw)
	snap.SessionID = c.sessionID
	snap.CollectedAt = time.Now().UnixMilli()
	snap.CPUUsage = c.diffCPU(cpuRaw)
	snap.NetIfaces = c.ratesFromCounters(counters, ips)

	hasData := snap.MemTotalMB > 0 || len(snap.DiskUsage) > 0 || len(snap.NetIfaces) > 0 || cpuRaw != nil
	if !hasData {
		msg := "部分指标暂不可用"
		if lastErr != nil {
			msg = fmt.Sprintf("%s（%v）", msg, lastErr)
		}
		c.emitSoftError(msg)
		return
	}

	// 有有效数据则清除瞬时错误；exit code / stderr 不阻断 partial
	snap.Error = ""
	c.mu.Lock()
	cp := snap
	c.lastGood = &cp
	c.mu.Unlock()

	if c.onSnap != nil {
		c.onSnap(snap)
	}
}

// runRemoteScript 在独立 exec 通道上用 sh -c 跑采集脚本（不占用主 PTY）。
func (c *SysInfoCollector) runRemoteScript() (string, error) {
	sess, err := c.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("创建采集会话失败: %w", err)
	}
	defer sess.Close()

	var stdout, stderr bytes.Buffer
	sess.Stdout = &stdout
	sess.Stderr = &stderr

	done := make(chan error, 1)
	go func() {
		// 固定 sh，避免用户 login shell（fish/csh 等）解析差异
		done <- sess.Run("sh -c " + shellSingleQuote(sysInfoScript))
	}()

	var runErr error
	select {
	case runErr = <-done:
	case <-time.After(sysInfoCollectTimeout):
		_ = sess.Close()
		runErr = fmt.Errorf("采集超时")
		<-done
	}

	out := stdout.String()
	// stderr 仅作补充诊断，绝不因 stderr/非零退出丢弃已有 stdout
	if strings.TrimSpace(out) == "" && stderr.Len() > 0 {
		errMsg := strings.TrimSpace(stderr.String())
		if len(errMsg) > 120 {
			errMsg = errMsg[:120] + "…"
		}
		if runErr != nil {
			return "", fmt.Errorf("%v; stderr: %s", runErr, errMsg)
		}
		return "", fmt.Errorf("stderr: %s", errMsg)
	}
	return out, runErr
}

// emitSoftError 短暂失败时保留上次成功快照，避免状态栏被「空错误包」盖掉。
func (c *SysInfoCollector) emitSoftError(msg string) {
	c.mu.Lock()
	prev := c.lastGood
	c.mu.Unlock()
	if prev != nil {
		snap := *prev
		snap.Error = msg
		snap.CollectedAt = time.Now().UnixMilli()
		if c.onSnap != nil {
			c.onSnap(snap)
		}
		return
	}
	if c.onError != nil {
		c.onError(msg)
	}
}

func (c *SysInfoCollector) diffCPU(cur *cpuSample) float64 {
	if cur == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	prev := c.prevCPU
	c.prevCPU = cur
	if prev == nil || cur.total <= prev.total {
		return 0
	}
	dt := float64(cur.total - prev.total)
	di := float64(cur.idle - prev.idle)
	if dt <= 0 {
		return 0
	}
	u := (1 - di/dt) * 100
	if u < 0 {
		u = 0
	}
	if u > 100 {
		u = 100
	}
	return u
}

func (c *SysInfoCollector) ratesFromCounters(counters []netCounter, ips map[string]string) []models.NetIface {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	// 合并：/proc/net/dev 计数 + 仅出现在 ip addr 中的网卡（保证带 IP 的物理口不被漏）
	byName := make(map[string]netCounter, len(counters)+len(ips))
	for _, n := range counters {
		byName[n.name] = n
	}
	for name := range ips {
		if name == "" || name == "lo" {
			continue
		}
		if _, ok := byName[name]; !ok {
			byName[name] = netCounter{name: name}
		}
	}

	ranked := rankNetIfaces(byName, ips)
	out := make([]models.NetIface, 0, len(ranked))
	for _, n := range ranked {
		item := models.NetIface{Name: n.name, IP: ips[n.name]}
		if prev, ok := c.prevNet[n.name]; ok {
			dt := now.Sub(prev.at).Seconds()
			if dt > 0.2 {
				if n.rx >= prev.rx {
					item.RxMbps = float64(n.rx-prev.rx) * 8 / dt / 1e6
				}
				if n.tx >= prev.tx {
					item.TxMbps = float64(n.tx-prev.tx) * 8 / dt / 1e6
				}
			}
		}
		c.prevNet[n.name] = netSample{rx: n.rx, tx: n.tx, at: now}
		out = append(out, item)
	}
	return out
}

type netCounter struct {
	name   string
	rx, tx uint64
}

// parseSysInfoLoose 分段解析，单段失败不影响其他段。
func parseSysInfoLoose(raw string) (models.SysInfoSnapshot, *cpuSample, []netCounter, map[string]string) {
	var snap models.SysInfoSnapshot
	section := func(name string) string {
		parts := strings.SplitN(raw, "---"+name+"---", 2)
		if len(parts) < 2 {
			return ""
		}
		body := parts[1]
		if i := strings.Index(body, "---"); i >= 0 {
			body = body[:i]
		}
		return body
	}

	cpuRaw := parseCPUStat(section("CPU"))
	snap.MemUsedMB, snap.MemTotalMB = parseMeminfo(section("MEM"))
	snap.DiskUsage = parseDisk(section("DISK"))
	counters := parseNetRaw(section("NET"))
	ips := parseIPAddr(section("IP"))
	snap.Uptime = parseUptime(section("UP"))
	return snap, cpuRaw, counters, ips
}

func parseCPUStat(s string) *cpuSample {
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		// cpu user nice system idle iowait irq softirq steal ...
		if len(fields) < 5 {
			return nil
		}
		var total uint64
		for i := 1; i < len(fields); i++ {
			total += parseUint(fields[i])
		}
		idle := parseUint(fields[4])
		if len(fields) > 5 {
			idle += parseUint(fields[5]) // iowait
		}
		return &cpuSample{total: total, idle: idle}
	}
	return nil
}

func parseMeminfo(s string) (used, total uint64) {
	var memTotal, memAvailable, memFree, buffers, cached uint64
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line := sc.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v := parseUint(fields[1]) // kB
		switch fields[0] {
		case "MemTotal:":
			memTotal = v
		case "MemAvailable:":
			memAvailable = v
		case "MemFree:":
			memFree = v
		case "Buffers:":
			buffers = v
		case "Cached:":
			cached = v
		}
	}
	if memTotal == 0 {
		return 0, 0
	}
	total = memTotal / 1024
	if memAvailable > 0 {
		used = (memTotal - memAvailable) / 1024
	} else {
		used = (memTotal - memFree - buffers - cached) / 1024
	}
	return used, total
}

func parseDisk(s string) []models.DiskInfo {
	var out []models.DiskInfo
	sc := bufio.NewScanner(strings.NewReader(s))
	headerSkipped := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if !headerSkipped {
			if strings.Contains(line, "Filesystem") || strings.Contains(line, "文件系统") {
				headerSkipped = true
			}
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		mount := fields[len(fields)-1]
		if mount == "tmpfs" || mount == "devtmpfs" ||
			strings.HasPrefix(mount, "/run") || strings.HasPrefix(mount, "/sys") || strings.HasPrefix(mount, "/proc") {
			continue
		}
		totalKB := parseUint(fields[1])
		usedKB := parseUint(fields[2])
		pctStr := strings.TrimSuffix(fields[4], "%")
		pct, _ := strconv.ParseFloat(pctStr, 64)
		info := models.DiskInfo{
			MountPoint: mount,
			TotalGB:    totalKB / (1024 * 1024),
			UsedGB:     usedKB / (1024 * 1024),
			UsagePct:   pct,
		}
		if info.TotalGB == 0 && totalKB > 0 {
			info.TotalGB = 1
		}
		out = append(out, info)
		if len(out) >= 16 {
			break
		}
	}
	return out
}

const netIfaceSoftCap = 64 // K8s 节点虚拟网卡极多，需足够上限以免漏掉 ens*/eth*

func parseNetRaw(s string) []netCounter {
	var out []netCounter
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "Inter-") || strings.HasPrefix(line, "face") {
			continue
		}
		colon := strings.Index(line, ":")
		if colon <= 0 {
			continue
		}
		name := strings.TrimSpace(line[:colon])
		if name == "" || name == "lo" {
			continue
		}
		fields := strings.Fields(line[colon+1:])
		if len(fields) < 9 {
			continue
		}
		out = append(out, netCounter{name: name, rx: parseUint(fields[0]), tx: parseUint(fields[8])})
	}
	return out
}

// rankNetIfaces 排序并裁剪：物理口/带 IP 优先，过滤海量 veth 噪声，避免 ens18 等被截掉。
func rankNetIfaces(byName map[string]netCounter, ips map[string]string) []netCounter {
	type scored struct {
		n netCounter
		p int
	}
	list := make([]scored, 0, len(byName))
	for _, n := range byName {
		ip := ips[n.name]
		pri := netIfacePriority(n.name, ip != "")
		// 纯虚拟口且无 IP：默认丢弃，避免上百个 veth 挤爆列表
		if pri >= 90 && ip == "" {
			continue
		}
		list = append(list, scored{n: n, p: pri})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].p != list[j].p {
			return list[i].p < list[j].p
		}
		return list[i].n.name < list[j].n.name
	})
	if len(list) > netIfaceSoftCap {
		list = list[:netIfaceSoftCap]
	}
	out := make([]netCounter, len(list))
	for i, s := range list {
		out[i] = s.n
	}
	return out
}

// netIfacePriority 越小越优先展示。
func netIfacePriority(name string, hasIP bool) int {
	n := strings.ToLower(name)
	virtualNoise := strings.HasPrefix(n, "veth") ||
		strings.HasPrefix(n, "cali") ||
		strings.HasPrefix(n, "flannel") ||
		strings.HasPrefix(n, "cni") ||
		strings.HasPrefix(n, "nodelocaldns") ||
		strings.HasPrefix(n, "tunl") ||
		strings.HasPrefix(n, "ip6tnl") ||
		strings.HasPrefix(n, "sit") ||
		strings.HasPrefix(n, "gre") ||
		strings.HasPrefix(n, "gretap") ||
		strings.HasPrefix(n, "erspan") ||
		strings.HasPrefix(n, "dummy")
	physical := strings.HasPrefix(n, "ens") ||
		strings.HasPrefix(n, "enp") ||
		strings.HasPrefix(n, "eno") ||
		strings.HasPrefix(n, "eth") ||
		strings.HasPrefix(n, "wlan") ||
		strings.HasPrefix(n, "wlp") ||
		strings.HasPrefix(n, "bond") ||
		strings.HasPrefix(n, "em")
	bridgeOrVirt := strings.HasPrefix(n, "br") ||
		n == "docker0" ||
		n == "cni0" ||
		strings.HasPrefix(n, "virbr") ||
		strings.HasPrefix(n, "kube-") ||
		strings.HasPrefix(n, "ovn")

	switch {
	case physical && hasIP:
		return 0
	case physical:
		return 1
	case hasIP && !virtualNoise:
		return 2
	case bridgeOrVirt && hasIP:
		return 3
	case hasIP:
		return 4
	case bridgeOrVirt:
		return 5
	case virtualNoise:
		return 90
	default:
		return 50
	}
}

// parseIPAddr 解析 `ip -o -4 addr`：2: eth0    inet 10.0.0.1/24 ...
func parseIPAddr(s string) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 {
			continue
		}
		// index name family addr
		name := strings.TrimSuffix(fields[1], ":")
		var ip string
		for i, f := range fields {
			if f == "inet" && i+1 < len(fields) {
				ip = fields[i+1]
				if j := strings.Index(ip, "/"); j > 0 {
					ip = ip[:j]
				}
				break
			}
		}
		if name != "" && name != "lo" && ip != "" {
			if _, exists := out[name]; !exists {
				out[name] = ip
			}
		}
	}
	return out
}

func parseUptime(s string) string {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) == 0 {
		return ""
	}
	sec, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return strings.TrimSpace(s)
	}
	d := time.Duration(sec) * time.Second
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("up %d days, %d:%02d", days, hours, mins)
	}
	return fmt.Sprintf("up %d:%02d", hours, mins)
}

func parseUint(s string) uint64 {
	n, _ := strconv.ParseUint(s, 10, 64)
	return n
}

// shellSingleQuote 生成适合 sh -c 的单引号包裹串。
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
