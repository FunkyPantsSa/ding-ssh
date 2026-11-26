package debugsrv

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	OpExecStart           = "exec.start"
	OpExecPlan            = "exec.plan"
	OpExecBatch           = "exec.batch"
	OpJobsGet             = "jobs.get"
	OpJobsWait            = "jobs.wait"
	OpJobsOutput          = "jobs.output"
	OpJobsCancel          = "jobs.cancel"
	OpJobsList            = "jobs.list"
	OpSessionsGet         = "sessions.get"
	OpSessionsWait        = "sessions.wait"
	OpHostChallenges      = "hostTrust.challenges"
	OpHostsMetrics        = "hosts.metrics"
	OpProcessesList       = "processes.list"
	OpTunnelsHealth       = "tunnels.health"
	OpServicesStatus      = "services.status"
	OpServicesLogs        = "services.logs"
	OpServicesRestart     = "services.restart"
	OpSessionsConnect     = "sessions.connect"
	OpSessionsDisconnect  = "sessions.disconnect"
	OpSessionsReconnect   = "sessions.reconnect"
	OpSessionsClose       = "sessions.close"
	OpSessionsCloseStatus = "sessions.closeStatus"
	OpSessionSnapshot     = "session.snapshot" // Host-only exact-tab state.
	OpServersSearch       = "servers.search"
)

// JobsExecutor pins and validates the target before the asynchronous job begins.
// A job owns its independent channel and must not type into the user's terminal.
type JobsExecutor interface {
	PrepareJob(context.Context, JobRequest) (JobRunner, error)
}
type JobRunner func(context.Context, func(string, []byte)) JobExecutionResult
type JobStateDirectory interface{ JobStateDirectory() string }
type JobRequest struct {
	SessionID      string            `json:"sessionId,omitempty"`
	Local          bool              `json:"local,omitempty"`
	Command        string            `json:"command"`
	Mode           string            `json:"mode,omitempty"`
	Cwd            string            `json:"cwd,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	TimeoutMs      int               `json:"timeoutMs"`
	MaxBytes       int               `json:"maxBytes"`
	IdempotencyKey string            `json:"idempotencyKey,omitempty"`
	Shell          string            `json:"shell,omitempty"`
}
type JobExecutionResult struct {
	ExitCode          *int   `json:"exitCode,omitempty"`
	Signal            string `json:"signal,omitempty"`
	Known             bool   `json:"known"`
	Cancelled         bool   `json:"cancelled"`
	CancelRequested   bool   `json:"cancelRequested"`
	TerminationProven bool   `json:"terminationProven"`
	Error             string `json:"error,omitempty"`
}
type jobChunk struct {
	Cursor int64  `json:"cursor"`
	Stream string `json:"stream"`
	Data   []byte `json:"base64"`
}
type jobView struct {
	ID                string `json:"jobId"`
	Owner             string `json:"-"`
	BatchID           string `json:"batchId,omitempty"`
	SessionID         string `json:"sessionId,omitempty"`
	Local             bool   `json:"local"`
	State             string `json:"state"`
	CreatedAt         int64  `json:"createdAt"`
	StartedAt         int64  `json:"startedAt,omitempty"`
	FinishedAt        int64  `json:"finishedAt,omitempty"`
	OutputBytes       int64  `json:"outputBytes"`
	CapturedBytes     int64  `json:"capturedBytes"`
	Truncated         bool   `json:"truncated"`
	ExitCode          *int   `json:"exitCode,omitempty"`
	Signal            string `json:"signal,omitempty"`
	Error             string `json:"error,omitempty"`
	ErrorCode         string `json:"errorCode,omitempty"`
	CancelRequested   bool   `json:"cancelRequested"`
	TerminationProven bool   `json:"terminationProven"`
}
type job struct {
	view        jobView
	chunks      []jobChunk
	maxBytes    int
	fingerprint string
	idem        string
	cancel      context.CancelFunc
	done        chan struct{}
	complete    bool
}
type diskJob struct {
	View        jobView    `json:"job"`
	Owner       string     `json:"owner"`
	Chunks      []jobChunk `json:"chunks"`
	Fingerprint string     `json:"fingerprint"`
	Idempotency string     `json:"idempotency"`
}
type jobManager struct {
	mu          sync.Mutex
	jobs        map[string]*job
	running     int
	closed      bool
	dir         string
	server      *Server
	stop        chan struct{}
	janitorDone chan struct{}
	workers     sync.WaitGroup
	plans       map[string]*jobPlan
}

func init() {
	registerJobsCaps()
}
func registerJobsCaps() {
	RegisterToolCap(OpExecStart, CapTerminalInput)
	RegisterToolCap(OpExecPlan, CapTerminalInput)
	RegisterToolCap(OpExecBatch, CapTerminalInput)
	RegisterToolCap(OpServicesStatus, CapTerminalInput, CapTerminalRead)
	RegisterToolCap(OpServicesLogs, CapTerminalInput, CapTerminalRead)
	registerWrite(OpServicesRestart, CapTerminalInput, OpServicesRestart)
	RegisterToolCap(OpSessionsConnect, CapConnect)
	RegisterToolCap(OpSessionsDisconnect, CapDisconnect)
	RegisterToolCap(OpSessionsReconnect, CapConnect, CapDisconnect)
	RegisterToolCap(OpSessionsClose, CapDisconnect)
	RegisterToolCap(OpSessionsCloseStatus, CapRead)
	RegisterToolCap(OpServersSearch, CapRead)
	for _, name := range []string{OpJobsGet, OpJobsWait, OpJobsOutput, OpJobsCancel, OpJobsList, OpSessionsGet, OpSessionsWait, OpHostChallenges, OpTunnelsHealth} {
		RegisterToolCap(name, CapRead)
	}
	// These fixed collectors open an SSH exec channel, never a PTY.
	RegisterToolCap(OpHostsMetrics, CapTerminalInput)
	RegisterToolCap(OpProcessesList, CapTerminalInput)
}
func jobsToolEntries() []mcpTool {
	id := map[string]any{"jobId": strProp("exec.start 返回的任务 id")}
	list := []mcpTool{
		{Name: OpSessionsConnect, Description: "推荐连接入口：连接保存的服务器或显式 local=true 打开本机终端。直接返回 sessionId/tabId，并等待连接就绪；waitMs=0 立即返回，等待超时不取消连接。不会批准主机信任。", InputSchema: obj(map[string]any{"serverId": strProp("已保存服务器id"), "local": boolProp("显式打开本机终端"), "waitMs": intProp("等待就绪毫秒，默认10000，范围0–30000")})},
		{Name: OpSessionsDisconnect, Description: "断开指定会话并保留标签，等待后端断开；会中断该会话中的作业。返回 sessionId/tabId/status。", InputSchema: obj(map[string]any{"sessionId": strProp("会话或标签id"), "waitMs": intProp("等待断开毫秒，默认10000，范围0–30000")}, "sessionId")},
		{Name: OpSessionsReconnect, Description: "重连指定会话并等待就绪；会影响原连接中的作业，保留标签缓冲。按目标分别要求本机执行或凭据使用授权。", InputSchema: obj(map[string]any{"sessionId": strProp("会话或标签id"), "waitMs": intProp("等待就绪毫秒，默认10000，范围0–30000")}, "sessionId")},
		{Name: OpSessionsClose, Description: "关闭精确标签并等待后端会话清理，遵守用户的关闭确认设置。需用户确认时返回 confirmation_required 和下一步，不依赖 DOM；不会代替用户批准。", InputSchema: obj(map[string]any{"sessionId": strProp("会话或标签id"), "waitMs": intProp("等待清理毫秒，默认10000，范围0–30000")}, "sessionId")},
		{Name: OpSessionsCloseStatus, Description: "观察指定标签关闭请求，返回 confirmation_required、cancelled 或 closed；仅观察，不关闭标签，不批准确认。", InputSchema: obj(map[string]any{"sessionId": strProp("会话或标签id"), "waitMs": intProp("等待关闭毫秒，默认0，范围0–30000")}, "sessionId")},
		{Name: OpServersSearch, Description: "按名称/地址/用户/分组搜索已保存服务器并分页，结果默认脱敏；查询不会匹配密码或私钥。", InputSchema: obj(map[string]any{"query": strProp("大小写不敏感子串"), "group": strProp("精确分组路径"), "offset": intProp("从0起的偏移"), "limit": intProp("每页1–500，默认100")})},
		{Name: OpServicesStatus, Description: "通过独立SSH通道读取Linux systemd指定服务的结构化状态，不修改服务。需精确unit名，不支持任意命令。", InputSchema: obj(map[string]any{"sessionId": strProp("SSH会话id"), "unit": strProp("systemd unit名，例如 nginx.service")}, "sessionId", "unit")},
		{Name: OpServicesLogs, Description: "读取Linux journald指定服务的最近日志，输出受256KiB限制，不占用户终端。仅支持有journald读取权限的当前SSH用户。", InputSchema: obj(map[string]any{"sessionId": strProp("SSH会话id"), "unit": strProp("systemd unit名"), "lines": intProp("日志条数，默认100，范围1–1000")}, "sessionId", "unit")},
		{Name: OpServicesRestart, Description: "请求重启精确的Linux systemd服务；必须用户批准，异步返回jobId。使用当前SSH用户权限，不自动sudo，不输入用户终端。完成后另调services.status验证状态。", InputSchema: obj(map[string]any{"sessionId": strProp("SSH会话id"), "unit": strProp("systemd unit名"), "token": strProp("用户批准后的确认凭据"), "idempotencyKey": strProp("可选重试键")}, "sessionId", "unit")},
		{Name: OpExecPlan, Description: "预检多主机独立执行计划，不执行命令。显式选择最多8个已连接的 SSH sessionId，冻结每个当前连接、命令与执行预算。计划仅当前身份可用，120秒过期；返回命令摘要与目标。", InputSchema: obj(map[string]any{"sessionIds": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "maxItems": 8}, "command": strProp("每个目标执行的相同命令"), "mode": map[string]any{"type": "string", "enum": []string{"raw", "posix"}}, "cwd": strProp("工作目录，仅posix"), "env": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}, "timeoutMs": intProp("每个目标含排队的总预算，默认30000，最多600000"), "maxBytes": intProp("每目标输出预算，默认262144，最多1048576")}, "sessionIds", "command")},
		{Name: OpExecBatch, Description: "执行已预检的精确计划，最多4个目标同时运行。计划一次性，重复调用返回相同批次；每个目标有独立jobId/状态，部分失败不会伪装为全体成功。通过jobs.cancel取消单目标，取消排队目标不会执行。", InputSchema: obj(map[string]any{"planId": strProp("exec.plan返回的id"), "concurrency": intProp("并发数，默认2，范围1–4")}, "planId")},
		{Name: OpExecStart, Description: "推荐命令执行入口：独立非 PTY 通道执行，不输入用户终端。waitMs>0 可在同一次调用中等待并返回输出与退出码；超时返回 jobId 可继续 jobs.wait。显式 local=true 才运行本机 shell，Windows 默认 cmd，可选 powershell/pwsh；远端 shell 由服务器决定。任务独立于 HTTP 请求，断线不假定进程已停止。idempotencyKey 绑定完整执行参数。", InputSchema: obj(map[string]any{"sessionId": strProp("已连接的 SSH sessionId；local=true 时省略"), "local": boolProp("显式请求本机独立 shell；默认 false"), "shell": map[string]any{"type": "string", "enum": []string{"cmd", "powershell", "pwsh", "sh"}, "description": "仅本机；Windows 默认 cmd，POSIX 默认 sh，支持情况见 app.health"}, "command": strProp("命令文本"), "mode": map[string]any{"type": "string", "enum": []string{"raw", "posix"}, "description": "远端 raw 保持 shell 命令，远端 cwd/env 需 posix；本机 raw 原生支持 cwd/env"}, "cwd": strProp("工作目录"), "env": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}, "timeoutMs": intProp("执行预算，默认 30000，范围 200–600000"), "waitMs": intProp("本次请求等待毫秒，默认0，范围0–30000；等待超时不取消任务"), "includeOutput": boolProp("返回 stdout/stderr；waitMs>0 时默认true"), "maxBytes": intProp("总输出存储上限，默认 262144，范围 1024–1048576"), "idempotencyKey": strProp("重试时复用的键，最多 200 字符")}, "command")},
		{Name: OpJobsGet, Description: "查询自己的独立执行任务状态与退出码。unknown 表示无法确认远端结果；cancel_requested 不是已终止。", InputSchema: obj(id, "jobId")},
		{Name: OpJobsWait, Description: "等待自己的任务完成，最多 30 秒；等待超时不会取消任务。includeOutput=true 同时返回捕获的 stdout/stderr 和游标。", InputSchema: obj(map[string]any{"jobId": id["jobId"], "timeoutMs": intProp("等待毫秒，默认 1000，范围 0–30000"), "includeOutput": boolProp("返回已捕获输出，默认false")}, "jobId")},
		{Name: OpJobsOutput, Description: "按字节游标分页读取自己的任务 stdout/stderr；nextCursor 可继续读，truncated 明确表示存储预算截断。", InputSchema: obj(map[string]any{"jobId": id["jobId"], "cursor": intProp("捕获输出中的字节游标，默认 0"), "maxBytes": intProp("本页预算，默认 65536，范围 1024–262144")}, "jobId")},
		{Name: OpJobsCancel, Description: "请求取消自己的任务。只关闭该任务的 SSH 通道；远端没有退出确认时状态为 unknown，不能保证其子进程已停止。本机进程取消会等待退出。", InputSchema: obj(id, "jobId")},
		{Name: OpJobsList, Description: "列出当前身份的最近独立执行任务；最多保留 64 条，完成任务保留一小时。", InputSchema: obj(nil)},
		{Name: OpSessionsGet, Description: "读取后端会话摘要，区分 sessionId/serverId/tabId；不会建立连接。", InputSchema: obj(map[string]any{"sessionId": strProp("会话 id")}, "sessionId")},
		{Name: OpSessionsWait, Description: "等待已存在的后端会话达到指定状态；不会建立连接或批准主机信任。", InputSchema: obj(map[string]any{"sessionId": strProp("会话 id"), "status": map[string]any{"type": "string", "enum": []string{"connected", "closed", "disconnected", "connecting"}}, "timeoutMs": intProp("等待毫秒，默认 1000，最多 30000")}, "sessionId")},
		{Name: OpHostChallenges, Description: "列出尚未过期的主机指纹确认请求；仅供观测，必须由用户在可信界面确认，工具不能批准或替换指纹。", InputSchema: obj(nil)},
		{Name: OpHostsMetrics, Description: "用独立 SSH exec 通道采集 Linux 主机结构化系统快照；最多 10 秒，不占终端。返回采样时间和不支持字段。", InputSchema: obj(map[string]any{"sessionId": strProp("SSH 会话 id")}, "sessionId")},
		{Name: OpProcessesList, Description: "采集 Linux 主机进程快照；独立 SSH 通道，不占终端。", InputSchema: obj(map[string]any{"sessionId": strProp("SSH 会话 id")}, "sessionId")},
		{Name: OpTunnelsHealth, Description: "读取当前隧道运行状态与配置端点；不主动拨测远端目标，不宣称连接健康等于服务可用。", InputSchema: obj(map[string]any{"id": strProp("可选隧道 id")})},
	}
	for i := range list {
		list[i].OutputSchema = outputObj(nil)
		list[i].structured = structuredIdentity
	}
	return list
}
func isJobsTool(name string) bool {
	switch name {
	case OpExecStart, OpExecPlan, OpExecBatch, OpServicesStatus, OpServicesLogs, OpServicesRestart, OpSessionsConnect, OpSessionsDisconnect, OpSessionsReconnect, OpSessionsClose, OpSessionsCloseStatus, OpServersSearch, OpJobsGet, OpJobsWait, OpJobsOutput, OpJobsCancel, OpJobsList, OpSessionsGet, OpSessionsWait, OpHostChallenges, OpHostsMetrics, OpProcessesList, OpTunnelsHealth:
		return true
	}
	return false
}
func IsJobsHostOp(name string) bool {
	switch name {
	case OpSessionsGet, OpSessionsWait, OpSessionSnapshot, OpHostChallenges, OpHostsMetrics, OpProcessesList, OpTunnelsHealth:
		return true
	}
	return false
}

func (s *Server) jobStore() *jobManager {
	s.jobsOnce.Do(func() {
		m := &jobManager{jobs: map[string]*job{}, plans: map[string]*jobPlan{}, server: s, stop: make(chan struct{}), janitorDone: make(chan struct{})}
		if d, ok := s.opts.Handler.(JobStateDirectory); ok {
			m.dir = d.JobStateDirectory()
			m.load()
		}
		s.jobs = m
		go m.janitor()
	})
	return s.jobs
}
func jobID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return "job_" + hex.EncodeToString(b)
}
func jobArgInt(args map[string]any, key string, def, min, max int) (int, error) {
	v, ok := args[key]
	if !ok {
		return def, nil
	}
	f, ok := v.(float64)
	if !ok {
		if n, yes := v.(int); yes {
			f = float64(n)
			ok = true
		}
	}
	if !ok || f != float64(int(f)) || f < float64(min) || f > float64(max) {
		return 0, fmt.Errorf("%w: %s 必须为 %d–%d 的整数", ErrBadInput, key, min, max)
	}
	return int(f), nil
}
func parseJobRequest(args map[string]any) (JobRequest, error) {
	b, err := json.Marshal(withoutToken(args))
	if err != nil {
		return JobRequest{}, err
	}
	var req JobRequest
	if err = json.Unmarshal(b, &req); err != nil {
		return req, fmt.Errorf("%w: exec 参数类型无效", ErrBadInput)
	}
	if strings.TrimSpace(req.Command) == "" || len(req.Command) > 256<<10 || strings.ContainsRune(req.Command, 0) {
		return req, fmt.Errorf("%w: command 无效或超过 256 KiB", ErrBadInput)
	}
	if req.Local && req.SessionID != "" || !req.Local && req.SessionID == "" {
		return req, fmt.Errorf("%w: 必须指定 sessionId 或显式 local=true，不能同时指定", ErrBadInput)
	}
	if len(req.IdempotencyKey) > 200 {
		return req, fmt.Errorf("%w: idempotencyKey 超过 200 字符", ErrBadInput)
	}
	if req.TimeoutMs, err = jobArgInt(args, "timeoutMs", 30000, 200, 600000); err != nil {
		return req, err
	}
	req.MaxBytes, err = jobArgInt(args, "maxBytes", 262144, 1024, 1048576)
	return req, err
}
func jobJSON(v any) ([]any, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	return []any{map[string]any{"type": "text", "text": string(b)}}, nil
}
func (s *Server) handleJobsTool(ctx context.Context, name string, args map[string]any) ([]any, error) {
	if name == OpServersSearch {
		if _, ok := args["limit"]; !ok {
			copyArgs := make(map[string]any, len(args)+1)
			for key, value := range args {
				copyArgs[key] = value
			}
			copyArgs["limit"] = 100
			args = copyArgs
		}
		v, e := s.appctlServersList(ctx, s.opts.Handler, args)
		if e != nil {
			return nil, e
		}
		return jobJSON(v)
	}
	if isSessionWorkflowTool(name) {
		return s.handleSessionWorkflow(ctx, name, args)
	}
	if name == OpServicesStatus || name == OpServicesLogs || name == OpServicesRestart {
		return s.handleServiceTool(ctx, name, args)
	}
	if IsJobsHostOp(name) {
		b, _ := json.Marshal(args)
		v, e := s.opts.Handler.Handle(ctx, name, b)
		if e != nil {
			return nil, e
		}
		return jobJSON(v)
	}
	m := s.jobStore()
	owner := CallerID(ctx)
	if name == OpExecPlan {
		return s.prepareJobPlan(ctx, args)
	}
	if name == OpExecBatch {
		return s.runJobPlan(ctx, args)
	}
	if name == OpExecStart {
		return s.startJobResult(ctx, args)
	}
	if name == OpJobsList {
		m.mu.Lock()
		m.sweepLocked()
		items := []jobView{}
		for _, j := range m.jobs {
			if j.view.Owner == owner {
				items = append(items, j.view)
			}
		}
		m.mu.Unlock()
		sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
		return jobJSON(map[string]any{"jobs": items, "count": len(items), "retentionSec": 3600})
	}
	id, _ := args["jobId"].(string)
	if id == "" {
		return nil, fmt.Errorf("%w: 缺少 jobId", ErrBadInput)
	}
	m.mu.Lock()
	j := m.jobs[id]
	if j == nil || j.view.Owner != owner {
		m.mu.Unlock()
		return nil, fmt.Errorf("%w: 任务不存在或不属于当前客户端", ErrNotFound)
	}
	done := j.done
	m.mu.Unlock()
	if name == OpJobsWait {
		ms, e := jobArgInt(args, "timeoutMs", 1000, 0, 30000)
		if e != nil {
			return nil, e
		}
		timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch name {
	case OpJobsGet, OpJobsWait:
		return jobJSON(jobResultLocked(j, appctlBool(args, "includeOutput")))
	case OpJobsCancel:
		if !j.complete {
			j.view.CancelRequested = true
			j.view.State = "cancel_requested"
			j.cancel()
		}
		return jobJSON(j.view)
	case OpJobsOutput:
		cursor, e := jobArgInt(args, "cursor", 0, 0, 1048576)
		if e != nil {
			return nil, e
		}
		limit, e := jobArgInt(args, "maxBytes", 65536, 1024, 262144)
		if e != nil {
			return nil, e
		}
		if int64(cursor) > j.view.CapturedBytes {
			return nil, fmt.Errorf("%w: cursor 超过已捕获输出", ErrBadInput)
		}
		chunks := []map[string]any{}
		next := int64(cursor)
		remaining := limit
		for _, c := range j.chunks {
			end := c.Cursor + int64(len(c.Data))
			if end <= next {
				continue
			}
			start := int(next - c.Cursor)
			if start < 0 {
				start = 0
			}
			data := c.Data[start:]
			if len(data) > remaining {
				data = data[:remaining]
			}
			if len(data) == 0 {
				break
			}
			chunks = append(chunks, map[string]any{"cursor": next, "stream": c.Stream, "text": strings.ToValidUTF8(string(data), "�"), "base64": base64.StdEncoding.EncodeToString(data), "bytes": len(data)})
			next += int64(len(data))
			remaining -= len(data)
			if remaining == 0 {
				break
			}
		}
		return jobJSON(map[string]any{"jobId": id, "chunks": chunks, "cursor": cursor, "nextCursor": next, "complete": j.complete, "hasMore": next < j.view.CapturedBytes, "truncated": j.view.Truncated, "outputBytes": j.view.OutputBytes, "capturedBytes": j.view.CapturedBytes})
	}
	return nil, fmt.Errorf("未知任务工具 %s", name)
}
func (m *jobManager) start(ctx context.Context, owner string, req JobRequest) (jobView, error) {
	return m.startPrepared(ctx, owner, req, nil, nil, "")
}
func (m *jobManager) startPrepared(ctx context.Context, owner string, req JobRequest, prepared JobRunner, semaphore chan struct{}, batchID string) (jobView, error) {
	fingerprintBytes, _ := json.Marshal(req)
	sum := sha256.Sum256(fingerprintBytes)
	fingerprint := hex.EncodeToString(sum[:])
	m.mu.Lock()
	m.sweepLocked()
	for _, j := range m.jobs {
		if req.IdempotencyKey != "" && j.view.Owner == owner && j.idem == req.IdempotencyKey {
			if j.fingerprint != fingerprint {
				m.mu.Unlock()
				return jobView{}, fmt.Errorf("%w: idempotencyKey 已绑定不同参数", ErrBadInput)
			}
			v := j.view
			m.mu.Unlock()
			return v, nil
		}
	}
	if m.closed || m.running >= 8 || len(m.jobs) >= 64 {
		m.mu.Unlock()
		return jobView{}, errors.New("任务预算已满或服务已停止；最多 8 个并发任务、64 个保留任务")
	}
	id := jobID()
	if id == "" {
		m.mu.Unlock()
		return jobView{}, errors.New("生成任务 id 失败")
	}
	jobCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(req.TimeoutMs)*time.Millisecond)
	j := &job{view: jobView{ID: id, Owner: owner, BatchID: batchID, SessionID: req.SessionID, Local: req.Local, State: "queued", CreatedAt: time.Now().UnixMilli()}, maxBytes: req.MaxBytes, fingerprint: fingerprint, idem: req.IdempotencyKey, cancel: cancel, done: make(chan struct{})}
	m.jobs[id] = j
	m.running++
	m.workers.Add(1)
	m.mu.Unlock()
	executor, ok := m.server.opts.Handler.(JobsExecutor)
	if !ok && prepared == nil {
		cancel()
		m.finish(j, JobExecutionResult{Known: true, Error: "宿主未接入独立执行器"})
		m.workers.Done()
		return jobView{}, errors.New("宿主未接入独立执行器")
	}
	runner := prepared
	var err error
	if runner == nil {
		runner, err = executor.PrepareJob(ctx, req)
	}
	if err != nil {
		cancel()
		m.finish(j, JobExecutionResult{Known: true, Error: err.Error()})
		m.workers.Done()
		m.mu.Lock()
		v := j.view
		m.mu.Unlock()
		return v, err
	}
	m.mu.Lock()
	if semaphore == nil {
		j.view.State = "running"
		j.view.StartedAt = time.Now().UnixMilli()
	}
	v := j.view
	m.mu.Unlock()
	if err := m.persist(j); err != nil {
		cancel()
		m.finish(j, JobExecutionResult{Known: true, TerminationProven: true, Error: "任务状态无法安全保存，命令未执行"})
		m.workers.Done()
		return jobView{}, fmt.Errorf("任务状态无法安全保存，命令未执行: %w", err)
	}
	m.publish(v)
	go func() {
		defer m.workers.Done()
		defer cancel()
		defer func() {
			if recover() != nil {
				m.finish(j, JobExecutionResult{Known: false, Error: "任务执行器异常，结果未知"})
			}
		}()
		if semaphore != nil {
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-jobCtx.Done():
				m.finish(j, JobExecutionResult{Known: true, Cancelled: true, CancelRequested: true, TerminationProven: true, Error: "排队期间已取消，命令未执行"})
				return
			}
			if jobCtx.Err() != nil {
				m.finish(j, JobExecutionResult{Known: true, Cancelled: true, CancelRequested: true, TerminationProven: true, Error: "排队期间已取消，命令未执行"})
				return
			}
			m.mu.Lock()
			j.view.State = "running"
			j.view.StartedAt = time.Now().UnixMilli()
			m.mu.Unlock()
		}
		if jobCtx.Err() != nil {
			m.finish(j, JobExecutionResult{Known: true, Cancelled: true, CancelRequested: true, TerminationProven: true, Error: "执行前已取消，命令未执行"})
			return
		}
		r := runner(jobCtx, func(stream string, b []byte) { m.append(j, stream, b) })
		if jobCtx.Err() != nil {
			r.CancelRequested = true
		}
		m.finish(j, r)
	}()
	return v, nil
}
func (m *jobManager) append(j *job, stream string, b []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j.complete || (stream != "stdout" && stream != "stderr") {
		return
	}
	j.view.OutputBytes += int64(len(b))
	left := j.maxBytes - int(j.view.CapturedBytes)
	if len(b) > left {
		b = b[:left]
		j.view.Truncated = true
	}
	if len(b) == 0 {
		return
	}
	if len(j.chunks) >= 4096 {
		j.view.Truncated = true
		return
	}
	c := jobChunk{j.view.CapturedBytes, stream, append([]byte{}, b...)}
	n := len(j.chunks)
	if n > 0 && j.chunks[n-1].Stream == stream && len(j.chunks[n-1].Data)+len(b) <= 32768 {
		j.chunks[n-1].Data = append(j.chunks[n-1].Data, b...)
	} else {
		j.chunks = append(j.chunks, c)
	}
	j.view.CapturedBytes += int64(len(b))
}
func (m *jobManager) finish(j *job, r JobExecutionResult) {
	m.mu.Lock()
	if j.complete {
		m.mu.Unlock()
		return
	}
	j.view.FinishedAt = time.Now().UnixMilli()
	j.view.ExitCode = r.ExitCode
	j.view.Signal = r.Signal
	j.view.Error = r.Error
	j.view.CancelRequested = j.view.CancelRequested || r.CancelRequested
	j.view.TerminationProven = r.TerminationProven
	switch {
	case r.Cancelled && r.TerminationProven:
		j.view.State = "cancelled"
		j.view.ErrorCode = "CANCELLED"
	case !r.Known:
		j.view.State = "unknown"
		j.view.ErrorCode = "RESULT_UNKNOWN"
	case r.Error != "" || r.ExitCode != nil && *r.ExitCode != 0:
		j.view.State = "failed"
		j.view.ErrorCode = "EXEC_FAILED"
	default:
		j.view.State = "succeeded"
	}
	j.complete = true
	m.running--
	close(j.done)
	v := j.view
	m.mu.Unlock()
	_ = m.persist(j)
	m.publish(v)
}
func (m *jobManager) publish(v jobView) {
	if m.server.opts.Hub != nil { // Do not broadcast command output or owner identifiers.
		b, _ := json.Marshal(map[string]any{"jobId": v.ID, "state": v.State, "sessionId": v.SessionID})
		m.server.opts.Hub.Publish(Event{Topic: "jobs.changed", SessionID: v.SessionID, Data: b})
	}
}
func (m *jobManager) sweepLocked() {
	for id, p := range m.plans {
		if time.Now().After(p.expires) {
			delete(m.plans, id)
		}
	}
	now := time.Now().Add(-time.Hour).UnixMilli()
	for id, j := range m.jobs {
		if j.complete && j.view.FinishedAt < now {
			delete(m.jobs, id)
			if m.dir != "" {
				_ = os.Remove(filepath.Join(m.dir, id+".json"))
			}
		}
	}
}
func (s *Server) CloseJobs() {
	m := s.jobStore()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	close(m.stop)
	for _, j := range m.jobs {
		if !j.complete {
			j.view.CancelRequested = true
			j.cancel()
		}
	}
	m.mu.Unlock()
	if m.janitorDone != nil {
		<-m.janitorDone
	}
	m.workers.Wait()
}

// CancelClientJobs is called when the owner revokes a client's authorization.
func (s *Server) CancelClientJobs(owner string) {
	m := s.jobStore()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.view.Owner == owner && !j.complete {
			j.view.CancelRequested = true
			j.view.State = "cancel_requested"
			j.cancel()
		}
	}
	for id, p := range m.plans {
		if p.owner == owner {
			delete(m.plans, id)
		}
	}
}
func (m *jobManager) janitor() {
	defer close(m.janitorDone)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.mu.Lock()
			m.sweepLocked()
			m.mu.Unlock()
		case <-m.stop:
			return
		}
	}
}
func (m *jobManager) persist(j *job) error {
	if m.dir == "" {
		return nil
	}
	m.mu.Lock()
	d := diskJob{j.view, j.view.Owner, make([]jobChunk, len(j.chunks)), j.fingerprint, j.idem}
	for i, c := range j.chunks {
		d.Chunks[i] = jobChunk{c.Cursor, c.Stream, append([]byte{}, c.Data...)}
	}
	m.mu.Unlock()
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(m.dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("任务状态目录必须是独立的普通目录")
	}
	if err := os.Chmod(m.dir, 0700); err != nil {
		return err
	}
	b, e := json.Marshal(d)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(m.dir, ".job-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	_ = f.Chmod(0600)
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil && ce == nil {
		return os.Rename(name, filepath.Join(m.dir, j.view.ID+".json"))
	}
	if e != nil {
		return e
	}
	return ce
}
func (m *jobManager) load() {
	entries, e := os.ReadDir(m.dir)
	if e != nil {
		return
	}
	for _, entry := range entries {
		if len(m.jobs) >= 64 {
			break
		}
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "job_") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, e := entry.Info()
		if e != nil || !info.Mode().IsRegular() || info.Size() > 3<<20 {
			continue
		}
		f, e := os.Open(filepath.Join(m.dir, entry.Name()))
		if e != nil {
			continue
		}
		b, e := io.ReadAll(io.LimitReader(f, (3<<20)+1))
		_ = f.Close()
		if e != nil || len(b) > 3<<20 {
			continue
		}
		var d diskJob
		if json.Unmarshal(b, &d) != nil || d.View.ID+".json" != entry.Name() || d.Owner == "" {
			continue
		}
		// Treat a corrupt or oversized state file as unavailable instead of
		// letting forged cursors index outside an output slice.
		valid := len(d.Chunks) <= 4096
		var captured int64
		for _, c := range d.Chunks {
			if c.Cursor != captured || (c.Stream != "stdout" && c.Stream != "stderr") {
				valid = false
				break
			}
			captured += int64(len(c.Data))
			if captured > 1<<20 {
				valid = false
				break
			}
		}
		if !valid || captured != d.View.CapturedBytes {
			continue
		}
		if d.View.FinishedAt != 0 && d.View.FinishedAt < time.Now().Add(-time.Hour).UnixMilli() {
			_ = os.Remove(filepath.Join(m.dir, entry.Name()))
			continue
		}
		d.View.Owner = d.Owner
		recovered := d.View.FinishedAt == 0
		if recovered {
			d.View.State = "unknown"
			d.View.Error = "应用重启，无法确认先前远端任务结果"
			d.View.ErrorCode = "RESULT_UNKNOWN"
			d.View.FinishedAt = time.Now().UnixMilli()
			d.View.TerminationProven = false
			d.View.ExitCode = nil
			d.View.Signal = ""
		}
		done := make(chan struct{})
		close(done)
		j := &job{view: d.View, chunks: d.Chunks, fingerprint: d.Fingerprint, idem: d.Idempotency, complete: true, done: done}
		m.jobs[d.View.ID] = j
		if recovered {
			_ = m.persist(j) // Persist the recovery time so repeated restarts cannot extend retention.
		}
	}
}
