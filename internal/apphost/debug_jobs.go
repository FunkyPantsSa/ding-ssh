package apphost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"time"

	"ding-ssh/internal/appdir"
	"ding-ssh/internal/debugsrv"
	"ding-ssh/internal/sshx"
)

func (c *debugController) JobStateDirectory() string {
	return filepath.Join(appdir.ConfigDir(), "mcp-jobs")
}

// PreflightJob validates the same arguments as execution without scheduling a
// task, opening an SSH channel, or starting a local process.
func (c *debugController) PreflightJob(ctx context.Context, req debugsrv.JobRequest) error {
	if req.Local {
		if _, err := localJobCommand(ctx, req); err != nil {
			return fmt.Errorf("%w: %v", debugsrv.ErrBadInput, err)
		}
		return nil
	}
	if req.Shell != "" {
		return fmt.Errorf("%w: shell 仅支持 local=true", debugsrv.ErrBadInput)
	}
	if _, err := sshx.ExecCommand(sshx.ExecRequest{Command: req.Command, Mode: req.Mode, Cwd: req.Cwd, Env: req.Env}); err != nil {
		return fmt.Errorf("%w: %v", debugsrv.ErrBadInput, err)
	}
	if c.app.manager == nil {
		return fmt.Errorf("%w: SSH 会话不存在", debugsrv.ErrNotFound)
	}
	v := c.app.manager.SessionState(req.SessionID)
	if v["exists"] != true {
		return fmt.Errorf("%w: SSH 会话不存在", debugsrv.ErrNotFound)
	}
	if v["status"] != "connected" {
		return &debugsrv.OperationError{Code: "SESSION_NOT_READY", Message: "SSH 会话尚未连接", NextSteps: []string{"调用 sessions.wait 等待 connected，或 sessions.reconnect 重连"}}
	}
	return nil
}

func (c *debugController) PrepareJob(ctx context.Context, req debugsrv.JobRequest) (debugsrv.JobRunner, error) {
	gate := debugGate{c}
	if req.Local {
		if err := gate.Allow(debugsrv.CapLocalExec, "exec.start"); err != nil {
			return nil, err
		}
		if _, err := localJobCommand(ctx, req); err != nil {
			return nil, fmt.Errorf("%w: %v", debugsrv.ErrBadInput, err)
		}
		return func(ctx context.Context, emit func(string, []byte)) debugsrv.JobExecutionResult {
			return runLocalJobRequest(ctx, req, emit)
		}, nil
	}
	if err := gate.Allow(debugsrv.CapTerminalInput, req.SessionID); err != nil {
		return nil, err
	}
	if req.Shell != "" {
		return nil, fmt.Errorf("%w: shell 仅支持 local=true；远端 shell 由 SSH 服务端决定", debugsrv.ErrBadInput)
	}
	if c.app.manager == nil {
		return nil, errors.New("SSH 管理器不可用")
	}
	p, err := c.app.manager.PrepareExec(sshx.ExecRequest{SessionID: req.SessionID, Command: req.Command, Mode: req.Mode, Cwd: req.Cwd, Env: req.Env})
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, emit func(string, []byte)) debugsrv.JobExecutionResult {
		r := p.Run(ctx, emit)
		return debugsrv.JobExecutionResult{ExitCode: r.ExitCode, Signal: r.Signal, Known: r.Known, Cancelled: r.Cancelled, CancelRequested: r.CancelRequested, TerminationProven: r.TerminationProven, Error: r.Error}
	}, nil
}

type localJobWriter struct {
	stream string
	emit   func(string, []byte)
	mu     *sync.Mutex
}

func (w localJobWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.emit != nil {
		w.emit(w.stream, b)
	}
	return len(b), nil
}
func runLocalJob(ctx context.Context, command string, emit func(string, []byte)) debugsrv.JobExecutionResult {
	return runLocalJobRequest(ctx, debugsrv.JobRequest{Local: true, Command: command}, emit)
}

func runLocalJobRequest(ctx context.Context, req debugsrv.JobRequest, emit func(string, []byte)) debugsrv.JobExecutionResult {
	cmd, err := localJobCommand(ctx, req)
	if err != nil {
		return debugsrv.JobExecutionResult{Known: true, TerminationProven: true, Error: err.Error()}
	}
	configureJobProcess(cmd)
	cmd.WaitDelay = time.Second
	var mu sync.Mutex
	cmd.Stdout = localJobWriter{"stdout", emit, &mu}
	cmd.Stderr = localJobWriter{"stderr", emit, &mu}
	cmd.Stdin = nil
	err = cmd.Run()
	result := debugsrv.JobExecutionResult{Known: true, TerminationProven: cmd.ProcessState != nil}
	if cmd.ProcessState != nil {
		code := cmd.ProcessState.ExitCode()
		result.ExitCode = &code
	}
	if ctx.Err() != nil {
		result.CancelRequested = true
		result.Cancelled = cmd.ProcessState != nil
		result.Error = ctx.Err().Error()
		return result
	}
	if err != nil {
		result.Error = err.Error()
		if cmd.ProcessState == nil {
			result.TerminationProven = true
		}
	}
	return result
}

func (c *debugController) sessionJobView(id string) map[string]any {
	if c.app.local != nil && c.app.local.Has(id) {
		return map[string]any{"sessionId": id, "tabId": id, "local": true, "exists": true, "status": "connected", "independentExecSupported": false, "note": "本机独立任务使用 exec.start local=true，与该终端无关"}
	}
	if c.app.manager == nil {
		return map[string]any{"sessionId": id, "status": "closed", "exists": false}
	}
	v := c.app.manager.SessionState(id)
	v["tabId"] = id
	return v
}
func (c *debugController) handleJobsHostOp(ctx context.Context, op string, raw json.RawMessage) (any, error) {
	args := map[string]any{}
	if len(raw) > 0 && json.Unmarshal(raw, &args) != nil {
		return nil, debugsrv.ErrBadInput
	}
	id := ctlArgStr(args, "sessionId")
	switch op {
	case debugsrv.OpSessionSnapshot:
		if id == "" {
			return nil, fmt.Errorf("%w: 缺少 sessionId", debugsrv.ErrBadInput)
		}
		return c.sessionSnapshot(ctx, id)
	case debugsrv.OpSessionsGet, debugsrv.OpSessionsWait:
		if id == "" {
			return nil, fmt.Errorf("%w: 缺少 sessionId", debugsrv.ErrBadInput)
		}
		want := ctlArgStr(args, "status")
		if want == "" {
			want = "connected"
		}
		switch want {
		case "connected", "connecting", "closed", "disconnected":
		default:
			return nil, fmt.Errorf("%w: status 无效", debugsrv.ErrBadInput)
		}
		ms := 1000
		if rawMS, ok := args["timeoutMs"]; ok {
			value, number := rawMS.(float64)
			if !number || value < 0 || value > 30000 || value != float64(int(value)) {
				return nil, fmt.Errorf("%w: timeoutMs 必须是 0–30000 的整数", debugsrv.ErrBadInput)
			}
			ms = int(value)
		}
		deadline := time.NewTimer(time.Duration(ms) * time.Millisecond)
		defer deadline.Stop()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			v, err := c.sessionSnapshot(ctx, id)
			if err != nil {
				return nil, err
			}
			v["matched"] = v["status"] == want
			if op == debugsrv.OpSessionsGet || v["status"] == want {
				return v, nil
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-deadline.C:
				return v, nil
			case <-ticker.C:
			}
		}
	case debugsrv.OpHostChallenges:
		items := sshx.PendingHostTrustChallenges()
		return map[string]any{"challenges": items, "count": len(items), "requiresUserApproval": true}, nil
	case debugsrv.OpHostsMetrics, debugsrv.OpProcessesList:
		if id == "" {
			return nil, fmt.Errorf("%w: 缺少 sessionId", debugsrv.ErrBadInput)
		}
		if err := (debugGate{c}).Allow(debugsrv.CapTerminalInput, id); err != nil {
			return nil, err
		}
		if c.app.manager == nil {
			return nil, debugsrv.ErrNotFound
		}
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		snap, err := c.app.manager.CollectSnapshot(ctx, id)
		if err != nil {
			return nil, err
		}
		if op == debugsrv.OpProcessesList {
			return map[string]any{"sessionId": id, "processes": snap.Processes, "available": snap.TopAvailable, "collectedAt": snap.CollectedAt, "source": "linux-procps-top", "error": snap.Error}, nil
		}
		return map[string]any{"sessionId": id, "snapshot": snap, "collectedAt": snap.CollectedAt, "source": "linux-fixed-collector", "unavailable": []string{"network rate requires two samples"}}, nil
	case debugsrv.OpTunnelsHealth:
		if c.app.manager == nil {
			return map[string]any{"tunnels": []any{}, "count": 0}, nil
		}
		items := c.app.manager.ListTunnels()
		target := ctlArgStr(args, "id")
		out := []any{}
		for _, t := range items {
			if target != "" && t.ID != target {
				continue
			}
			out = append(out, map[string]any{"tunnel": t, "running": t.Status == "running", "activeProbe": false, "serviceReachability": "unknown"})
		}
		if target != "" && len(out) == 0 {
			return nil, debugsrv.ErrNotFound
		}
		return map[string]any{"tunnels": out, "count": len(out), "collectedAt": time.Now().UnixMilli()}, nil
	}
	return nil, debugsrv.ErrNotFound
}

var _ debugsrv.JobsExecutor = (*debugController)(nil)
var _ io.Writer = localJobWriter{}
