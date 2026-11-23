package debugsrv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

type jobPlan struct {
	mu          sync.Mutex
	id, owner   string
	expires     time.Time
	requests    []JobRequest
	runners     []JobRunner
	targets     []any
	commandHash string
	started     bool
	result      map[string]any
}

func planRequests(args map[string]any) ([]JobRequest, error) {
	var ids []string
	switch list := args["sessionIds"].(type) {
	case []string:
		ids = append(ids, list...)
	case []any:
		for _, v := range list {
			id, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("%w: sessionIds 必须为字符串数组", ErrBadInput)
			}
			ids = append(ids, id)
		}
	}
	if len(ids) < 1 || len(ids) > 8 {
		return nil, fmt.Errorf("%w: 必须显式选择1–8个SSH会话", ErrBadInput)
	}
	seen := map[string]bool{}
	out := make([]JobRequest, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			return nil, fmt.Errorf("%w: 会话不能为空或重复", ErrBadInput)
		}
		seen[id] = true
		single := map[string]any{}
		for k, v := range args {
			if k != "sessionIds" {
				single[k] = v
			}
		}
		single["sessionId"] = id
		single["local"] = false
		req, e := parseJobRequest(single)
		if e != nil {
			return nil, e
		}
		out = append(out, req)
	}
	return out, nil
}
func jobRequestArgs(r JobRequest) map[string]any {
	b, _ := json.Marshal(r)
	var args map[string]any
	_ = json.Unmarshal(b, &args)
	return args
}

// Called by policy.go before generic scalar-target checks; every frozen target
// is authorized independently both at planning and execution.
func (s *Server) authorizeJobBatch(ctx context.Context, tool string, args map[string]any) error {
	var reqs []JobRequest
	if tool == OpExecPlan {
		var e error
		reqs, e = planRequests(args)
		if e != nil {
			return e
		}
	} else {
		id, _ := args["planId"].(string)
		m := s.jobStore()
		m.mu.Lock()
		p := m.plans[id]
		m.mu.Unlock()
		if p == nil || p.owner != CallerID(ctx) || time.Now().After(p.expires) {
			return fmt.Errorf("%w: 执行计划不存在或过期", ErrNotFound)
		}
		reqs = p.requests
	}
	for _, req := range reqs {
		if err := s.authorizeInvocation(ctx, OpExecStart, jobRequestArgs(req)); err != nil {
			return err
		}
		if err := s.allowCap(CapTerminalInput, req.SessionID); err != nil {
			return err
		}
	}
	return nil
}
func (s *Server) prepareJobPlan(ctx context.Context, args map[string]any) ([]any, error) {
	if err := s.authorizeJobBatch(ctx, OpExecPlan, args); err != nil {
		return nil, err
	}
	reqs, err := planRequests(args)
	if err != nil {
		return nil, err
	}
	executor, ok := s.opts.Handler.(JobsExecutor)
	if !ok {
		return nil, errors.New("独立执行器不可用")
	}
	p := &jobPlan{id: jobID(), owner: CallerID(ctx), expires: time.Now().Add(120 * time.Second), requests: reqs, targets: []any{}}
	if p.id == "" {
		return nil, errors.New("生成计划id失败")
	}
	hash := sha256.Sum256([]byte(reqs[0].Command))
	p.commandHash = hex.EncodeToString(hash[:])
	for _, req := range reqs {
		runner, e := executor.PrepareJob(ctx, req)
		if e != nil {
			return nil, fmt.Errorf("目标 %s 预检失败：%w", req.SessionID, e)
		}
		p.runners = append(p.runners, runner)
		target := any(map[string]any{"sessionId": req.SessionID})
		raw, _ := json.Marshal(map[string]any{"sessionId": req.SessionID})
		if value, e := s.opts.Handler.Handle(ctx, OpSessionsGet, raw); e == nil && value != nil {
			target = value
		}
		p.targets = append(p.targets, target)
	}
	m := s.jobStore()
	m.mu.Lock()
	m.sweepLocked()
	if m.closed || len(m.plans) >= 32 {
		m.mu.Unlock()
		return nil, errors.New("执行计划数量已达上限32或服务已停止")
	}
	m.plans[p.id] = p
	m.mu.Unlock()
	return jobJSON(map[string]any{"planId": p.id, "dryRun": true, "targets": p.targets, "count": len(reqs), "commandSHA256": p.commandHash, "commandBytes": len(reqs[0].Command), "expiresAt": p.expires.UnixMilli(), "executionStarted": false, "note": "目标连接和命令已冻结；执行前重新检查当前授权。每目标结果独立，不支持跨主机事务回滚。"})
}
func (s *Server) runJobPlan(ctx context.Context, args map[string]any) ([]any, error) {
	if err := s.authorizeJobBatch(ctx, OpExecBatch, args); err != nil {
		return nil, err
	}
	concurrency, err := jobArgInt(args, "concurrency", 2, 1, 4)
	if err != nil {
		return nil, err
	}
	id, _ := args["planId"].(string)
	m := s.jobStore()
	m.mu.Lock()
	p := m.plans[id]
	m.mu.Unlock()
	if p == nil {
		return nil, ErrNotFound
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		return jobJSON(p.result)
	}
	m.mu.Lock()
	available := !m.closed && m.running+len(p.requests) <= 8 && len(m.jobs)+len(p.requests) <= 64
	m.mu.Unlock()
	if !available {
		return nil, errors.New("任务并发/存储预算不足；计划尚未执行，可稍后重试")
	}
	sem := make(chan struct{}, concurrency)
	results := []any{}
	p.started = true
	for i, req := range p.requests {
		req.IdempotencyKey = p.id + ":" + req.SessionID
		view, e := m.startPrepared(ctx, p.owner, req, p.runners[i], sem, p.id)
		if e != nil {
			results = append(results, map[string]any{"sessionId": req.SessionID, "submitted": false, "error": e.Error()})
			continue
		}
		results = append(results, map[string]any{"sessionId": req.SessionID, "submitted": true, "job": view})
	}
	p.result = map[string]any{"batchId": p.id, "planId": p.id, "targets": results, "concurrency": concurrency, "count": len(results), "commandSHA256": p.commandHash, "note": "任务已经提交；请逐一查询jobId。重复提交此计划返回同一批次，不能重复执行。"}
	return jobJSON(p.result)
}
