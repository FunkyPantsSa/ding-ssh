package debugsrv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
)

const OpAppPreflight = "app.preflight"

// OperationError carries recovery information without exposing tool arguments.
// OutcomeUnknown is explicit: retrying an uncertain write may duplicate it.
type OperationError struct {
	Code                string
	Message             string
	Cause               error
	Retryable           bool
	OutcomeUnknown      bool
	RetryAfterMs        int
	MissingCapabilities []string
	NextSteps           []string
}

func (e *OperationError) Error() string { return e.Message }
func (e *OperationError) Unwrap() error { return e.Cause }

// SessionTargetResolver resolves both live and disconnected tabs. It must never
// connect, mutate a tab or approve host trust. Unknown IDs must not become local.
type SessionTargetResolver interface {
	SessionTargetLocal(context.Context, string) (local bool, known bool, err error)
}

// JobPreflight checks platform/shell arguments without starting a process.
type JobPreflight interface {
	PreflightJob(context.Context, JobRequest) error
}

func init()                  { registerPreflightCaps() }
func registerPreflightCaps() { RegisterToolCap(OpAppPreflight, CapRead) }

func preflightToolEntries() []mcpTool {
	return []mcpTool{{Name: OpAppPreflight, Description: "Preflight one exact tool call without executing it or requesting approval. Returns all missing capabilities, client/target/path restrictions, argument errors and confirmation requirements. allowed=true means the checked policy permits the call; confirmationRequired still requires the normal approval flow. Remote existence, canonical symlink paths and execution success remain deferred to execution.", InputSchema: obj(map[string]any{"tool": strProp("Exact tool name"), "args": map[string]any{"type": "object", "additionalProperties": true, "description": "Exact arguments planned for the target tool; no secrets are echoed back"}}, "tool", "args"), OutputSchema: outputObj(map[string]any{"tool": outProp("string"), "allowed": outProp("boolean"), "requiredCapabilities": outProp("array"), "missingCapabilities": outProp("array"), "confirmationRequired": outProp("boolean"), "issues": outProp("array"), "nextSteps": outProp("array")}, "tool", "allowed", "requiredCapabilities", "missingCapabilities", "confirmationRequired", "issues", "nextSteps"), structured: structuredIdentity}}
}

func (s *Server) invocationCapabilities(ctx context.Context, tool string, args map[string]any) ([]Capability, error) {
	return s.invocationCapabilitiesForTarget(ctx, tool, args, true)
}

func (s *Server) invocationCapabilitiesForTarget(ctx context.Context, tool string, args map[string]any, resolveTarget bool) ([]Capability, error) {
	caps, _ := ToolCapabilities(tool)
	caps = append([]Capability{}, caps...)
	add := func(c Capability) {
		for _, current := range caps {
			if current == c {
				return
			}
		}
		caps = append(caps, c)
	}
	if tool == "eval_js" || unsafeUITool(tool) {
		add(CapTrustedAutomation)
	}
	if appctlBool(args, "includeSecrets") {
		add(CapSecretsRead)
	}
	switch tool {
	case "open_tab", OpSessionsConnect:
		if appctlBool(args, "local") {
			add(CapLocalExec)
		} else {
			add(CapCredentialUse)
		}
	case OpExecStart:
		if appctlBool(args, "local") {
			// The local independent executor does not type into a user terminal.
			// Drop the legacy static terminal capability and require only the
			// dedicated local execution grant.
			filtered := caps[:0]
			for _, c := range caps {
				if c != CapTerminalInput {
					filtered = append(filtered, c)
				}
			}
			caps = filtered
			add(CapLocalExec)
		}
	case "reconnect_session", OpSessionsReconnect, "send_input", OpTerminalRun:
		id := appctlString(args, "id")
		if tool == OpSessionsReconnect {
			id = appctlString(args, "sessionId")
		}
		reconnect := tool == "reconnect_session" || tool == OpSessionsReconnect
		if reconnect {
			// Old registrations may still include a credential capability. Target
			// resolution below is the source of truth for this conditional check.
			filtered := caps[:0]
			for _, c := range caps {
				if c != CapCredentialUse {
					filtered = append(filtered, c)
				}
			}
			caps = filtered
		}
		if resolver, ok := s.opts.Handler.(SessionTargetResolver); ok && id != "" && resolveTarget {
			local, known, err := resolver.SessionTargetLocal(ctx, id)
			if err != nil {
				return caps, err
			}
			if !known && reconnect {
				return caps, &OperationError{Code: "NOT_FOUND", Message: "会话不存在，请先通过 sessions.connect 建立会话", Cause: ErrNotFound, NextSteps: []string{"Use sessions.connect and keep its returned sessionId."}}
			}
			if known && local {
				add(CapLocalExec)
			} else if reconnect {
				add(CapCredentialUse)
			}
		} else if reconnect {
			add(CapCredentialUse)
		}
	}
	return caps, nil
}

func (s *Server) missingInvocationCaps(ctx context.Context, caps []Capability, detail string) ([]string, []string) {
	missing, reasons := []string{}, []string{}
	p, restricted := ctx.Value(callerKey{}).(ClientPolicy)
	for _, c := range caps {
		var reason string
		if err := s.allowCap(c, detail); err != nil {
			reason = err.Error()
		}
		if restricted && p.ID != "local-owner" && !hasString(p.Capabilities, string(c)) && c != CapRead {
			if reason != "" {
				reason += "; "
			}
			reason += fmt.Sprintf("客户端未获授权能力 %s", c)
		}
		if reason != "" {
			missing = append(missing, string(c))
			reasons = append(reasons, reason)
		}
	}
	return missing, reasons
}

func (s *Server) checkInvocationCaps(ctx context.Context, tool string, args map[string]any) error {
	caps, targetErr := s.invocationCapabilities(ctx, tool, args)
	missing, reasons := s.missingInvocationCaps(ctx, caps, argDetail(args))
	if len(missing) > 0 {
		return &OperationError{Code: "PERMISSION_DENIED", Message: strings.Join(reasons, "; "), Cause: ErrForbidden, MissingCapabilities: missing, NextSteps: []string{"Ask the user to enable every missing capability for the application and current client, then call app.preflight again."}}
	}
	return targetErr
}

func (s *Server) handlePreflight(ctx context.Context, args map[string]any) ([]any, error) {
	tool := appctlString(args, "tool")
	planned, ok := args["args"].(map[string]any)
	if tool == "" || !ok {
		return nil, fmt.Errorf("%w: tool 和对象 args 必须提供", ErrBadInput)
	}
	entry, known := toolEntryByName(tool)
	if !known {
		return nil, &OperationError{Code: "NOT_FOUND", Message: "未知工具，请先调用 tools.search 或 tools/list", Cause: ErrNotFound, NextSteps: []string{"Call tools.search to find the canonical tool name."}}
	}
	issues := []map[string]any{}
	next := []string{}
	addIssue := func(err error) {
		if err == nil {
			return
		}
		meta := toolErrorMeta(err)
		meta["message"] = err.Error()
		issues = append(issues, meta)
		if operation, ok := err.(*OperationError); ok {
			next = append(next, operation.NextSteps...)
		}
	}
	addIssue(validatePreflightSchema(planned, entry.InputSchema, "args"))
	policyErr := s.authorizeInvocationPolicy(ctx, tool, planned)
	addIssue(policyErr)
	// A scoped caller cannot use preflight to probe an unauthorized session.
	caps, targetErr := s.invocationCapabilitiesForTarget(ctx, tool, planned, policyErr == nil || !IsRestrictedCaller(ctx))
	addIssue(targetErr)
	missing, reasons := s.missingInvocationCaps(ctx, caps, argDetail(planned))
	if len(missing) > 0 {
		addIssue(&OperationError{Code: "PERMISSION_DENIED", Message: strings.Join(reasons, "; "), Cause: ErrForbidden, MissingCapabilities: missing, NextSteps: []string{"Ask the user to grant the missing application/client capabilities; MCP cannot grant its own permissions."}})
	}
	// Scoped callers must not learn host-wide allowlists from detailed policy
	// errors. Their client path scope was already checked by policy above.
	if policyErr == nil || !IsRestrictedCaller(ctx) {
		if err := s.preflightToolArgs(tool, planned); err != nil {
			addIssue(preflightErrorForCaller(ctx, err))
		}
		if err := s.preflightSudoArgs(tool, planned); err != nil {
			addIssue(preflightErrorForCaller(ctx, err))
		}
	}
	if tool == OpExecStart {
		req, err := parseJobRequest(planned)
		addIssue(err)
		if err == nil && policyErr == nil {
			if validator, yes := s.opts.Handler.(JobPreflight); yes {
				addIssue(validator.PreflightJob(ctx, req))
			}
		}
	}
	action, confirm := ToolConfirmAction(tool)
	if confirm {
		next = append(next, "Call confirm.prepare with the exact tool/action/args, obtain user approval, then invoke the original tool with the returned token.")
	}
	if len(issues) == 0 && !confirm {
		next = append(next, "Invoke the tool with these exact arguments.")
	}
	if len(issues) > 0 {
		next = append(next, "Resolve every issue, then run app.preflight again before execution.")
	}
	required := []string{}
	for _, c := range caps {
		required = append(required, string(c))
	}
	constraints := map[string]any{"checked": []string{"argumentSchema", "capabilities", "clientIdentity", "targetScope", "lexicalPathScope"}, "deferred": []string{"remote canonical path and symlink checks", "remote existence and connection availability", "execution result"}}
	if len(sftpWritePathArgs[tool]) > 0 && !IsRestrictedCaller(ctx) {
		constraints["writeAllowlist"] = append([]string{}, s.sftpAllowlist()...)
	}
	if p, restricted := ctx.Value(callerKey{}).(ClientPolicy); restricted && p.ID != "local-owner" {
		constraints["clientReadPaths"] = append([]string{}, p.ReadPaths...)
		constraints["clientWritePaths"] = append([]string{}, p.WritePaths...)
	}
	return jobJSON(map[string]any{"tool": tool, "allowed": len(issues) == 0, "requiredCapabilities": required, "missingCapabilities": missing, "confirmationRequired": confirm, "confirmationAction": action, "issues": issues, "constraints": constraints, "nextSteps": uniqueStrings(next)})
}

func classifyPreflightError(err error) error {
	if errors.Is(err, ErrBadInput) || errors.Is(err, ErrForbidden) {
		return err
	}
	return &OperationError{Code: "INVALID_ARGUMENT", Message: err.Error(), Cause: ErrBadInput, NextSteps: []string{"Correct the tool arguments using tools.search, then call app.preflight again."}}
}

func preflightErrorForCaller(ctx context.Context, err error) error {
	if IsRestrictedCaller(ctx) {
		var operation *OperationError
		if errors.As(err, &operation) && operation.Code == "PATH_NOT_ALLOWED" {
			return &OperationError{Code: operation.Code, Message: "远端路径不符合宿主级写入策略", Cause: ErrForbidden, NextSteps: []string{"Ask the application owner to review the host write policy; detailed host paths are not exposed to scoped clients."}}
		}
	}
	return classifyPreflightError(err)
}

func uniqueStrings(values []string) []string {
	out, seen := []string{}, map[string]bool{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

// Validate the subset used by our tool schemas. Domain-specific checks remain
// in shared validators above so preview and execution use the same boundaries.
func validatePreflightSchema(value any, schema map[string]any, field string) error {
	bad := func(message string) error { return fmt.Errorf("%w: %s %s", ErrBadInput, field, message) }
	if schema == nil {
		return nil
	}
	typeName, _ := schema["type"].(string)
	switch typeName {
	case "object":
		m, ok := value.(map[string]any)
		if !ok {
			return bad("must be an object")
		}
		props, _ := schema["properties"].(map[string]any)
		for _, key := range uiStringList(schema["required"]) {
			if _, present := m[key]; !present {
				return bad("requires " + key)
			}
		}
		keys := make([]string, 0, len(m))
		for key := range m {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			sub, exists := props[key]
			if !exists {
				if allow, explicit := schema["additionalProperties"].(bool); explicit && !allow {
					return bad("has unknown argument " + key)
				}
				sub = schema["additionalProperties"]
			}
			if child, yes := sub.(map[string]any); yes {
				if err := validatePreflightSchema(m[key], child, field+"."+key); err != nil {
					return err
				}
			}
		}
	case "string":
		if _, ok := value.(string); !ok {
			return bad("must be a string")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return bad("must be a boolean")
		}
	case "integer", "number":
		b, err := json.Marshal(value)
		if err != nil {
			return bad("must be a number")
		}
		var n float64
		if json.Unmarshal(b, &n) != nil || math.IsNaN(n) || math.IsInf(n, 0) || typeName == "integer" && math.Trunc(n) != n {
			return bad("must be an " + typeName)
		}
		if min, ok := schema["minimum"].(float64); ok && n < min {
			return bad("is below minimum")
		}
		if max, ok := schema["maximum"].(float64); ok && n > max {
			return bad("is above maximum")
		}
	case "array":
		v := reflect.ValueOf(value)
		if !v.IsValid() || v.Kind() != reflect.Slice {
			return bad("must be an array")
		}
		if min, ok := schema["minItems"].(int); ok && v.Len() < min {
			return bad("has too few items")
		}
		if max, ok := schema["maxItems"].(int); ok && v.Len() > max {
			return bad("has too many items")
		}
		if child, ok := schema["items"].(map[string]any); ok {
			for i := 0; i < v.Len(); i++ {
				if err := validatePreflightSchema(v.Index(i).Interface(), child, fmt.Sprintf("%s[%d]", field, i)); err != nil {
					return err
				}
			}
		}
	}
	if enum, ok := schema["enum"]; ok {
		v := reflect.ValueOf(enum)
		if v.Kind() == reflect.Slice {
			matched := false
			for i := 0; i < v.Len(); i++ {
				if reflect.DeepEqual(value, v.Index(i).Interface()) {
					matched = true
					break
				}
			}
			if !matched {
				return bad("has an unsupported value")
			}
		}
	}
	return nil
}
