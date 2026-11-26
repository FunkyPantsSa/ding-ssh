package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"ding-ssh/internal/appdir"
	"ding-ssh/internal/models"
	"ding-ssh/internal/store"
)

const profileMarker = ".ding-ssh-mcp-profile.json"

type launcherDeps struct {
	getenv func(string) string
	client *http.Client
	alive  func(int) bool
	start  func(string, string) (int, <-chan error, error)
}

func defaultLauncherDeps() launcherDeps {
	return launcherDeps{
		getenv: os.Getenv,
		client: &http.Client{Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		alive:  processAlive,
		start:  launchApplication,
	}
}

func validateLauncherOptions(o options) error {
	if o.launch != "" && o.launch != "never" && o.launch != "if-needed" {
		return errors.New("--launch 必须为 never 或 if-needed")
	}
	if o.waitReadyMs != 0 && (o.waitReadyMs < 100 || o.waitReadyMs > 120000) {
		return errors.New("--wait-ready-ms 必须为 100–120000")
	}
	if o.appPath != "" && !filepath.IsAbs(o.appPath) {
		return errors.New("--app-path 必须为绝对路径")
	}
	if o.launch == "if-needed" && o.appPath == "" {
		return errors.New("--launch=if-needed 必须指定 --app-path")
	}
	if o.profile != "" {
		if !filepath.IsAbs(o.profile) {
			return errors.New("--profile 必须为独立配置的绝对目录")
		}
		if o.url != "" || o.token != "" || o.debugRaw != "" {
			return errors.New("--profile 不可与 --url/--token/--debug-json 混用，避免附着错误实例")
		}
		if err := validateProfilePath(o.profile); err != nil {
			return err
		}
	}
	return nil
}

func strictAttachment(o options) bool {
	return o.launch == "if-needed" || o.profile != "" || o.expectedBuildID != "" || o.waitReadySet
}

func discoveryPath(o options) string {
	if o.profile != "" {
		return filepath.Join(o.profile, "debug.json")
	}
	if o.debugRaw != "" {
		return o.debugRaw
	}
	return defaultDebugInfoPath()
}

func normalConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "ding-ssh")
}

func canonicalPath(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	return filepath.Join(canonicalPath(parent), filepath.Base(path))
}

func validateProfilePath(path string) error {
	clean := canonicalPath(path)
	normal := normalConfigDir()
	if normal != "" {
		normal = canonicalPath(normal)
		rel, err := filepath.Rel(normal, clean)
		if err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
			return errors.New("--profile 不能使用日常用户配置目录或其子目录")
		}
	}
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("--profile 不可指向符号链接")
	}
	return nil
}

// Only the explicit, new isolated directory is provisioned. Existing settings
// are read without migrations or writes, including an intentionally disabled MCP.
func provisionProfile(path string) (bool, error) {
	if err := validateProfilePath(path); err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return false, err
	}
	if err := os.Mkdir(path, 0700); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return false, err
		}
		enabled, e := profileDebugEnabled(path)
		if e != nil {
			return false, e
		}
		if !enabled {
			return false, errors.New("DEBUG_DISABLED: 已有 profile 未启用调试；不会修改已有配置，请使用新的独立目录或在应用中启用")
		}
		return false, nil
	}
	db, err := store.OpenSQLite(filepath.Join(path, "ding-ssh.db"))
	if err != nil {
		return false, err
	}
	defer db.Close()
	settingsStore := store.NewSQLiteSettingsStore(db)
	settings, err := settingsStore.Get()
	if err != nil {
		return false, err
	}
	// This profile is explicitly created for developer verification. It contains
	// no user servers or credentials. Secret export, eval and remote writes stay off.
	settings.Debug = models.DebugSettings{
		Enabled: true, Port: 0, BindLAN: false,
		CapUIInspect: true, CapUIWrite: true, TrustedAutomation: true,
		CapTerminalRead: true, CapTerminalInput: true, CapLocalExec: true,
		CapConnect: true, CapDisconnect: true, CapTransferCancel: true,
		CapConfigWrite: true, AuditPersistent: true,
	}
	settings.AutoLoginLastServer = false
	settings.AutoStart = false
	settings.TrayIconEnabled = false
	if err := settingsStore.Save(settings); err != nil {
		return false, err
	}
	marker, _ := json.Marshal(map[string]any{"schemaVersion": 1, "kind": "isolated-developer-verification", "createdAt": time.Now().UTC().Format(time.RFC3339Nano), "profile": filepath.Clean(path)})
	if err := os.WriteFile(filepath.Join(path, profileMarker), marker, 0600); err != nil {
		return false, err
	}
	return true, nil
}

func profileDebugEnabled(path string) (bool, error) {
	dbPath := filepath.Join(path, "ding-ssh.db")
	if _, err := os.Stat(dbPath); err == nil {
		u := url.URL{Scheme: "file", Path: dbPath, RawQuery: "mode=ro"}
		db, err := sql.Open("sqlite", u.String())
		if err != nil {
			return false, err
		}
		defer db.Close()
		var raw string
		err = db.QueryRow("SELECT value FROM settings WHERE key='debug'").Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("读取已有 profile 调试设置失败（未修改）：%w", err)
		}
		var debug models.DebugSettings
		if err := json.Unmarshal([]byte(raw), &debug); err != nil {
			return false, err
		}
		return debug.Enabled, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	raw, err := os.ReadFile(filepath.Join(path, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var settings models.Settings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return false, err
	}
	return settings.Debug.Enabled, nil
}

func prepareTarget(parent context.Context, o options, log *logf2, deps launcherDeps) (config, error) {
	if !strictAttachment(o) {
		return resolveConfig(o, deps.getenv, false)
	}
	budget := o.waitReadyMs
	if budget == 0 {
		budget = 30000
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(budget)*time.Millisecond)
	defer cancel()
	if o.appPath != "" {
		binary, err := applicationBinary(o.appPath)
		if err != nil {
			return config{}, fmt.Errorf("NOT_INSTALLED: %w", err)
		}
		if o.expectedBuildID == "" {
			file, err := os.Open(binary)
			if err != nil {
				return config{}, err
			}
			hash := sha256.New()
			_, err = io.Copy(hash, file)
			file.Close()
			if err != nil {
				return config{}, err
			}
			o.expectedBuildID = fmt.Sprintf("%x", hash.Sum(nil))
		}
	}
	if o.profile != "" && o.launch == "if-needed" {
		created, err := provisionProfile(o.profile)
		if err != nil {
			return config{}, err
		}
		if created {
			log.infof("已创建独立验证配置：%s（仅本机、随机端口、无用户服务器/凭据）", o.profile)
		}
	}
	var launched bool
	var exit <-chan error
	var lastErr error
	for {
		cfg, err := resolveConfig(o, deps.getenv, false)
		liveDiscovery := cfg.discovery != nil && cfg.discovery.PID > 0 && deps.alive(cfg.discovery.PID)
		if err == nil {
			err = validateAttachment(ctx, cfg, o, deps)
			if err == nil {
				cfg.expectedBuildID = o.expectedBuildID
				log.infof("目标实例与前端已就绪：%s", cfg.discovery.InstanceID)
				return cfg, nil
			}
		}
		lastErr = err
		if launchFatal(err) {
			return config{}, err
		}
		if !launched && o.launch == "if-needed" && !liveDiscovery {
			if o.profile == "" && o.url == "" && o.token == "" {
				enabled, e := profileDebugEnabled(appdir.ConfigDir())
				if e != nil || !enabled {
					return config{}, errors.New("DEBUG_DISABLED: 日常实例未启用调试；请在应用中启用，或用 --profile 指定新的独立验证目录")
				}
			}
			pid, done, e := deps.start(o.appPath, o.profile)
			if e != nil {
				return config{}, fmt.Errorf("LAUNCH_FAILED: %w", e)
			}
			launched, exit = true, done
			log.infof("已启动指定应用，等待后端与前端就绪（pid=%d）", pid)
		}
		select {
		case <-ctx.Done():
			return config{}, fmt.Errorf("READY_TIMEOUT: %w；最后状态：%v", ctx.Err(), lastErr)
		case e := <-exit:
			return config{}, fmt.Errorf("LAUNCH_EXITED: 应用在 ready 前退出：%v", e)
		case <-time.After(150 * time.Millisecond):
		}
	}
}

func launchFatal(err error) bool {
	if err == nil {
		return false
	}
	for _, code := range []string{"INSTANCE_MISMATCH:", "BUILD_MISMATCH:", "DISCOVERY_IDENTITY_MISSING:", "INVALID_ENDPOINT:", "AUTH_FAILED:"} {
		if strings.Contains(err.Error(), code) {
			return true
		}
	}
	return false
}

func validateAttachment(ctx context.Context, cfg config, o options, deps launcherDeps) error {
	d := cfg.discovery
	if d == nil {
		return errors.New("DISCOVERY_IDENTITY_MISSING: 严格附着需要包含 pid/instanceId 的 debug.json；请勿混用显式 URL/token")
	}
	if d.PID <= 0 || !deps.alive(d.PID) {
		return errors.New("STALE_DISCOVERY: 发现文件对应的进程已退出")
	}
	if d.InstanceID == "" {
		return errors.New("DISCOVERY_IDENTITY_MISSING: 当前发现文件缺少 instanceId，请使用支持实例身份的应用构建")
	}
	parsed, err := url.Parse(cfg.url)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" || (parsed.Hostname() != "localhost" && (net.ParseIP(parsed.Hostname()) == nil || !net.ParseIP(parsed.Hostname()).IsLoopback())) {
		return errors.New("INVALID_ENDPOINT: launcher 只附着无额外路径的本机 loopback 调试服务")
	}
	profile := d.Profile
	if profile == "" {
		profile = d.ConfigDir
	}
	if o.profile != "" && (profile == "" || canonicalPath(profile) != canonicalPath(o.profile)) {
		return errors.New("INSTANCE_MISMATCH: 发现文件的 profile 与指定独立配置不符")
	}
	health, err := launcherJSON(ctx, deps.client, cfg, http.MethodGet, "/v1/health", nil)
	if err != nil {
		return err
	}
	if int(numberAt(health, "pid")) != d.PID || stringAt(health, "instanceId") != d.InstanceID {
		return errors.New("INSTANCE_MISMATCH: 健康接口的 pid/instanceId 与发现文件不符")
	}
	if d.ProcessStartID != "" && stringAt(health, "processStartId") != d.ProcessStartID {
		return errors.New("INSTANCE_MISMATCH: 进程启动身份不符")
	}
	build := stringAt(health, "buildId")
	if build == "" {
		build = stringAt(health, "backendBuildId")
	}
	if d.BuildID != "" && build != d.BuildID || o.expectedBuildID != "" && build != o.expectedBuildID {
		return errors.New("BUILD_MISMATCH: 当前运行构建与发现文件或 --expected-build-id 不符")
	}
	observation, err := launcherJSON(ctx, deps.client, cfg, http.MethodGet, "/v1/app/observe?includeImage=false&timeoutMs=1000", nil)
	if err != nil {
		return err
	}
	if observation["error"] != nil {
		return errors.New("FRONTEND_NOT_READY: app.observe 不可用")
	}
	if observedID := stringAt(observation, "instanceId"); observedID != "" && observedID != d.InstanceID {
		return errors.New("INSTANCE_MISMATCH: 观测属于另一实例")
	}
	frontend, _ := observation["frontend"].(map[string]any)
	if frontend["ready"] != true {
		return errors.New("FRONTEND_NOT_READY: 正在等待前端 mounted/ready 握手")
	}
	return nil
}

func launcherJSON(parent context.Context, client *http.Client, cfg config, method, path string, body any) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(parent, 1500*time.Millisecond)
	defer cancel()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, cfg.url+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, errors.New("AUTH_FAILED: 发现文件 token 被服务端拒绝")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("BACKEND_NOT_READY: HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 1<<20 {
		return nil, errors.New("BACKEND_NOT_READY: 状态响应超过 1MiB")
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("BACKEND_NOT_READY: 状态响应无效")
	}
	return value, nil
}

func stringAt(m map[string]any, key string) string  { s, _ := m[key].(string); return s }
func numberAt(m map[string]any, key string) float64 { n, _ := m[key].(float64); return n }

func launchApplication(path, profile string) (int, <-chan error, error) {
	binary, err := applicationBinary(path)
	if err != nil {
		return 0, nil, err
	}
	cmd := exec.Command(binary)
	cmd.Env = os.Environ()
	if profile != "" {
		prefix := appdir.EnvDevConfigDir + "="
		env := make([]string, 0, len(cmd.Env)+1)
		for _, item := range cmd.Env {
			if !strings.HasPrefix(item, prefix) {
				env = append(env, item)
			}
		}
		cmd.Env = append(env, prefix+profile)
	}
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		return 0, nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	return cmd.Process.Pid, done, nil
}

func applicationBinary(path string) (string, error) {
	binary := path
	if runtime.GOOS == "darwin" && strings.HasSuffix(strings.ToLower(path), ".app") {
		out, err := exec.Command("/usr/bin/plutil", "-extract", "CFBundleExecutable", "raw", "-o", "-", filepath.Join(path, "Contents", "Info.plist")).Output()
		if err != nil {
			return "", fmt.Errorf("无法读取 app bundle 可执行文件：%w", err)
		}
		name := strings.TrimSpace(string(out))
		if name == "" || filepath.Base(name) != name {
			return "", errors.New("app bundle 的 CFBundleExecutable 无效")
		}
		binary = filepath.Join(path, "Contents", "MacOS", name)
	}
	info, err := os.Stat(binary)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("指定应用不可执行：%s", binary)
	}
	return binary, nil
}
