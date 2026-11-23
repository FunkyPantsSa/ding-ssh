package debugsrv

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

var serviceUnit = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@:\-]{0,199}$`)

func serviceRequest(name string, args map[string]any) (JobRequest, error) {
	id, _ := args["sessionId"].(string)
	unit, _ := args["unit"].(string)
	if id == "" || !serviceUnit.MatchString(unit) {
		return JobRequest{}, fmt.Errorf("%w: sessionId不能为空，unit必须为有效的精确systemd名称", ErrBadInput)
	}
	req := JobRequest{SessionID: id, TimeoutMs: 10000, MaxBytes: 262144, Mode: "posix"}
	switch name {
	case OpServicesStatus:
		req.Command = "systemctl --no-pager --no-ask-password show --property=Id,LoadState,ActiveState,SubState,UnitFileState,MainPID,ExecMainStatus,Result -- '" + unit + "'"
	case OpServicesLogs:
		n, err := jobArgInt(args, "lines", 100, 1, 1000)
		if err != nil {
			return req, err
		}
		req.Command = fmt.Sprintf("journalctl --no-pager --output=json --lines=%d --unit='%s'", n, unit)
	case OpServicesRestart:
		req.Command = "systemctl --no-ask-password restart -- '" + unit + "'"
		req.TimeoutMs = 120000
		req.IdempotencyKey, _ = args["idempotencyKey"].(string)
		if len(req.IdempotencyKey) > 200 {
			return req, fmt.Errorf("%w: idempotencyKey超长", ErrBadInput)
		}
	default:
		return req, ErrNotFound
	}
	return req, nil
}
func (s *Server) handleServiceTool(ctx context.Context, name string, args map[string]any) ([]any, error) {
	req, err := serviceRequest(name, args)
	if err != nil {
		return nil, err
	}
	if name == OpServicesRestart {
		view, e := s.jobStore().start(ctx, CallerID(ctx), req)
		if e != nil {
			return nil, e
		}
		return jobJSON(view)
	}
	executor, ok := s.opts.Handler.(JobsExecutor)
	if !ok {
		return nil, fmt.Errorf("独立执行器不可用")
	}
	runner, err := executor.PrepareJob(ctx, req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMs)*time.Millisecond)
	defer cancel()
	var mu sync.Mutex
	out := []byte{}
	stderr := []byte{}
	truncated := false
	r := runner(ctx, func(stream string, b []byte) {
		mu.Lock()
		defer mu.Unlock()
		left := req.MaxBytes - len(out) - len(stderr)
		if len(b) > left {
			b = b[:left]
			truncated = true
		}
		if stream == "stdout" {
			out = append(out, b...)
		} else {
			stderr = append(stderr, b...)
		}
	})
	mu.Lock()
	text := strings.ToValidUTF8(string(out), "�")
	diagnostic := strings.ToValidUTF8(string(stderr), "�")
	wasTruncated := truncated
	mu.Unlock()
	result := map[string]any{"sessionId": req.SessionID, "unit": args["unit"], "collectedAt": time.Now().UnixMilli(), "source": "linux-systemd", "execution": r, "truncated": wasTruncated, "available": r.Known && r.ExitCode != nil && *r.ExitCode == 0}
	if diagnostic != "" {
		result["diagnostic"] = diagnostic
	}
	if name == OpServicesStatus {
		props := map[string]string{}
		for _, line := range strings.Split(text, "\n") {
			if key, value, ok := strings.Cut(line, "="); ok {
				props[key] = value
			}
		}
		result["properties"] = props
	} else {
		entries := []any{}
		parseErrors := 0
		for _, line := range strings.Split(text, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var value map[string]any
			if json.Unmarshal([]byte(line), &value) == nil {
				entries = append(entries, value)
			} else {
				parseErrors++
			}
		}
		result["entries"] = entries
		result["count"] = len(entries)
		result["parseErrors"] = parseErrors
		result["partial"] = wasTruncated || parseErrors > 0
	}
	return jobJSON(result)
}
