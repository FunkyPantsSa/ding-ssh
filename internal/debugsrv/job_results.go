package debugsrv

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Wait/output options describe delivery and are deliberately excluded from the
// persisted execution fingerprint: retrying a job with a longer wait must not
// execute the command twice. Validate these before starting any work.
func (s *Server) startJobResult(ctx context.Context, args map[string]any) ([]any, error) {
	waitMS, err := jobArgInt(args, "waitMs", 0, 0, 30000)
	if err != nil {
		return nil, err
	}
	if raw, ok := args["includeOutput"]; ok {
		if _, valid := raw.(bool); !valid {
			return nil, fmt.Errorf("%w: includeOutput 必须是 boolean", ErrBadInput)
		}
	}
	req, err := parseJobRequest(args)
	if err != nil {
		return nil, err
	}
	m := s.jobStore()
	v, err := m.start(ctx, CallerID(ctx), req)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	j := m.jobs[v.ID]
	done := j.done
	if waitMS == 0 {
		result := jobResultLocked(j, appctlBool(args, "includeOutput"))
		m.mu.Unlock()
		return jobJSON(result)
	}
	m.mu.Unlock()
	timer := time.NewTimer(time.Duration(waitMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	case <-ctx.Done():
		// The task is independent of this request. Return its resumable ID even
		// when the delivery wait is cancelled; never imply execution stopped.
	}
	include := true
	if raw, ok := args["includeOutput"]; ok {
		include, _ = raw.(bool)
	}
	m.mu.Lock()
	result := jobResultLocked(j, include)
	m.mu.Unlock()
	return jobJSON(result)
}

// Caller holds jobManager.mu. Output is bounded by the job's capture budget;
// base64 chunks in jobs.output remain the lossless interface for binary data.
func jobResultLocked(j *job, include bool) map[string]any {
	b, _ := json.Marshal(j.view)
	var result map[string]any
	_ = json.Unmarshal(b, &result)
	result["complete"] = j.complete
	result["outcomeKnown"] = j.complete && j.view.State != "unknown"
	if include {
		var stdout, stderr strings.Builder
		for _, chunk := range j.chunks {
			if chunk.Stream == "stderr" {
				stderr.Write(chunk.Data)
			} else {
				stdout.Write(chunk.Data)
			}
		}
		result["stdout"] = strings.ToValidUTF8(stdout.String(), "�")
		result["stderr"] = strings.ToValidUTF8(stderr.String(), "�")
		result["nextCursor"] = j.view.CapturedBytes
		result["outputEncoding"] = "utf-8"
	}
	if !j.complete {
		result["nextAction"] = map[string]any{"tool": OpJobsWait, "args": map[string]any{"jobId": j.view.ID, "includeOutput": true}}
	}
	return result
}
