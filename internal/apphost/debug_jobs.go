package apphost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"ding-ssh/internal/appdir"
	"ding-ssh/internal/debugsrv"
	"ding-ssh/internal/sshx"
)

func (c *debugController) JobStateDirectory() string {
	return filepath.Join(appdir.ConfigDir(), "mcp-jobs")
}

func (c *debugController) PrepareJob(ctx context.Context, req debugsrv.JobRequest) (debugsrv.JobRunner, error) {
	gate := debugGate{c}
	if err := gate.Allow(debugsrv.CapTerminalInput, req.SessionID); err != nil {
		return nil, err
	}
	if req.Local {
		if err := gate.Allow(debugsrv.CapLocalExec, "exec.start"); err != nil {
			return nil, err
		}
		command, err := sshx.ExecCommand(sshx.ExecRequest{Command: req.Command, Mode: req.Mode, Cwd: req.Cwd, Env: req.Env})
		if err != nil {
			return nil, err
		}
		if runtime.GOOS == "windows" && req.Mode == "posix" {
			return nil, errors.New("Windows 本机执行不支持 POSIX 模式")
		}
		return func(ctx context.Context, emit func(string, []byte)) debugsrv.JobExecutionResult {
			return runLocalJob(ctx, command, emit)
		}, nil
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
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/D", "/S", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", command)
	}
	configureJobProcess(cmd)
	cmd.WaitDelay = time.Second
	var mu sync.Mutex
	cmd.Stdout = localJobWriter{"stdout", emit, &mu}
	cmd.Stderr = localJobWriter{"stderr", emit, &mu}
	cmd.Stdin = nil
	err := cmd.Run()
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
			v := c.sessionJobView(id)
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
