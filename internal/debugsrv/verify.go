package debugsrv

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const (
	OpVerifyRun = "verify.run"
	OpVerifyGet = "verify.get"
)

func registerVerifyCaps() {
	RegisterToolCap(OpVerifyRun, CapUIInspect)
	RegisterToolCap(OpVerifyGet, CapUIInspect)
}
func verifyToolEntries() []mcpTool {
	list := []mcpTool{
		{Name: OpVerifyRun, Description: "运行有界真机观测场景desktop.boot（ready+原生像素+组件）或desktop.current（诊断当前页面）。异步返回jobId，报告保存在jobs.output；缺像素或不一致不能通过。无业务写入，取消不重放。", InputSchema: obj(map[string]any{"scenarioId": map[string]any{"type": "string", "enum": []string{"desktop.boot", "desktop.current"}}, "idempotencyKey": strProp("同一场景重试键"), "expectedBuildId": strProp("可选，要求当前二进制SHA256精确匹配")}, "scenarioId")},
		{Name: OpVerifyGet, Description: "读取自己的验证任务摘要及机器可读报告。与jobs.get/output/cancel共享生命周期和恢复语义。", InputSchema: obj(map[string]any{"jobId": strProp("verify.run返回的id")}, "jobId")},
	}
	for i := range list {
		list[i].OutputSchema = outputObj(nil)
		list[i].structured = structuredIdentity
	}
	return list
}
func (s *Server) handleVerify(ctx context.Context, name string, args map[string]any) ([]any, error) {
	if name == OpVerifyGet {
		summary, e := s.handleJobsTool(ctx, OpJobsGet, args)
		if e != nil {
			return nil, e
		}
		output, e := s.handleJobsTool(ctx, OpJobsOutput, map[string]any{"jobId": args["jobId"], "maxBytes": 262144})
		if e != nil {
			return nil, e
		}
		parse := func(c []any) any {
			if len(c) == 0 {
				return nil
			}
			var v any
			_ = json.Unmarshal([]byte(uiArgString(uiMap(c[0]), "text")), &v)
			return v
		}
		return jobJSON(map[string]any{"job": parse(summary), "output": parse(output)})
	}
	scenario := uiArgString(args, "scenarioId")
	if scenario != "desktop.boot" && scenario != "desktop.current" {
		return nil, fmt.Errorf("%w: unsupported scenario", ErrBadInput)
	}
	if expected := uiArgString(args, "expectedBuildId"); expected != "" {
		host := s.runtimeObservation(ctx)
		build := uiMap(host["build"])
		if expected != uiArgString(build, "binarySHA256") {
			return nil, fmt.Errorf("BUILD_MISMATCH: current instance does not match expected binary")
		}
	}
	req := JobRequest{Command: "verify:" + scenario + ":" + uiArgString(args, "expectedBuildId"), TimeoutMs: 20000, MaxBytes: 262144, IdempotencyKey: uiArgString(args, "idempotencyKey")}
	runner := func(runCtx context.Context, emit func(string, []byte)) JobExecutionResult {
		start := time.Now()
		r, e := s.handleObservation(runCtx, OpAppObserve, map[string]any{"includeImage": true, "timeoutMs": 2000, "ignoreTerminalOutput": true})
		report := map[string]any{"schemaVersion": "1", "scenarioId": scenario, "startedAt": start.UnixMilli(), "finishedAt": time.Now().UnixMilli(), "environment": "native-desktop", "inputMode": "none-read-only", "residualResources": []any{}, "status": "inconclusive"}
		code := 1
		if e != nil {
			report["error"] = e.Error()
		} else {
			d := r.Data
			capture := uiMap(d["capture"])
			frontend := uiMap(d["frontend"])
			ready := uiBool(frontend, "ready") && uiBool(d, "ready")
			pixels := capture["status"] == "captured" && len(r.Images) > 0
			consistent := capture["consistency"] == "stable-window"
			issues := uiMapSlice(d["issues"])
			_, errorsObserved := d["errors"]
			runtimeErrors := uiMapSlice(d["errors"])
			runtimeErrorsClear := errorsObserved && len(runtimeErrors) == 0
			componentsOK := true
			var componentCheck map[string]any
			if scenario == "desktop.boot" {
				tree, treeErr := uiFetch(runCtx, s.uiFrontend(), OpUITree, map[string]any{"limit": 1, "depth": 1})
				componentsOK = treeErr == nil && tree["source"] == "public-component-registry" && uiBool(tree, "ready") && len(uiMapSlice(tree["nodes"])) > 0
				componentCheck = map[string]any{"available": treeErr == nil, "passed": componentsOK}
				if treeErr == nil {
					componentCheck["source"] = tree["source"]
					componentCheck["nodeCount"] = len(uiMapSlice(tree["nodes"]))
				}
			}
			confirmed := 0
			for _, i := range issues {
				if i["confidence"] == "confirmed" && i["severity"] == "error" {
					confirmed++
				}
			}
			report["observationId"] = d["observationId"]
			report["host"] = d["host"]
			report["capture"] = capture
			report["coverage"] = d["coverage"]
			report["issues"] = issues
			report["errors"] = d["errors"]
			assertions := []any{map[string]any{"name": "frontend.ready", "passed": ready}, map[string]any{"name": "native.pixels", "passed": pixels}, map[string]any{"name": "capture.consistency", "passed": consistent}, map[string]any{"name": "confirmed.render.errors", "passed": confirmed == 0}, map[string]any{"name": "runtime.errors.clear", "passed": runtimeErrorsClear, "observed": errorsObserved}}
			if scenario == "desktop.boot" {
				assertions = append(assertions, map[string]any{"name": "components.registered", "passed": componentsOK})
				report["components"] = componentCheck
			}
			report["assertions"] = assertions
			if ready && pixels && consistent && confirmed == 0 && runtimeErrorsClear && componentsOK && runCtx.Err() == nil {
				report["status"] = "passed"
				code = 0
			} else if confirmed > 0 || len(runtimeErrors) > 0 {
				report["status"] = "failed"
			}
			stored, imageStored := s.verificationArtifactState(runCtx, uiArgString(d, "observationId"))
			report["artifacts"] = map[string]any{"observationId": d["observationId"], "readWith": "app.diagnostics", "observationStored": stored, "imageStored": imageStored, "retentionSec": 3600, "availability": "checked-at-report-time"}
		}
		raw, _ := json.Marshal(report)
		emit("stdout", append(raw, '\n'))
		return JobExecutionResult{Known: true, TerminationProven: true, ExitCode: &code, Cancelled: runCtx.Err() != nil}
	}
	view, e := s.jobStore().startPrepared(ctx, CallerID(ctx), req, runner, nil, "")
	if e != nil {
		return nil, e
	}
	return jobJSON(view)
}

func (s *Server) verificationArtifactState(ctx context.Context, id string) (bool, bool) {
	s.obsMu.Lock()
	defer s.obsMu.Unlock()
	stored, ok := s.observations[id]
	if !ok || stored.Owner != CallerID(ctx) || time.Now().UnixMilli()-stored.At > 3600000 {
		return false, false
	}
	return true, len(stored.PNG) > 0
}
