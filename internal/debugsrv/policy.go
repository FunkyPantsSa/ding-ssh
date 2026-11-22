package debugsrv

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type callerKey struct{}

// CallerID is an authorization identity, never an MCP transport session ID.
func CallerID(ctx context.Context) string {
	if p, ok := ctx.Value(callerKey{}).(ClientPolicy); ok {
		return p.ID
	}
	return "local-owner"
}

func WithCaller(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, callerKey{}, ClientPolicy{ID: id})
}

// ClientPolicy is immutable once issued. An empty target list means no access
// to that kind of target, unless AllTargets was explicitly authorized.
type ClientPolicy struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	ServerIDs    []string `json:"serverIds"`
	SessionIDs   []string `json:"sessionIds"`
	ReadPaths    []string `json:"readPaths"`
	WritePaths   []string `json:"writePaths"`
	AllTargets   bool     `json:"allTargets"`
	ExpiresAt    int64    `json:"expiresAt"`
}

type clientEntry struct {
	policy ClientPolicy
	hash   [32]byte
}
type ClientRegistry struct {
	mu      sync.Mutex
	entries map[string]clientEntry
}

func NewClientRegistry() *ClientRegistry {
	return &ClientRegistry{entries: make(map[string]clientEntry)}
}

func (r *ClientRegistry) Issue(p ClientPolicy) (ClientPolicy, string, error) {
	if r == nil {
		return p, "", errors.New("客户端注册表不可用")
	}
	if p.ExpiresAt <= time.Now().UnixMilli() || p.ExpiresAt > time.Now().Add(24*time.Hour).UnixMilli() {
		return p, "", errors.New("客户端有效期必须在未来 24 小时以内")
	}
	for _, c := range p.Capabilities {
		if _, ok := KnownCapability(c); !ok {
			return p, "", fmt.Errorf("未知能力 %q", c)
		}
		if c == string(CapEval) || c == string(CapTrustedAutomation) || c == string(CapLocalExec) || c == string(CapSecretsRead) {
			return p, "", errors.New("受限客户端不支持任意 JS、本机执行或明文凭据导出；请使用本机所有者的可信连接")
		}
	}
	for _, root := range append(append([]string{}, p.ReadPaths...), p.WritePaths...) {
		if _, err := NormalizeRemotePath(root); err != nil {
			return p, "", err
		}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return p, "", err
	}
	token := "dsc_" + hex.EncodeToString(key)
	p.ID = "client_" + hex.EncodeToString(key[:8])
	p.Capabilities = append([]string(nil), p.Capabilities...)
	p.ServerIDs = append([]string(nil), p.ServerIDs...)
	p.SessionIDs = append([]string(nil), p.SessionIDs...)
	p.ReadPaths = append([]string(nil), p.ReadPaths...)
	p.WritePaths = append([]string(nil), p.WritePaths...)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweepLocked()
	if len(r.entries) >= 32 {
		return p, "", errors.New("客户端数量已达上限 32")
	}
	r.entries[p.ID] = clientEntry{cloneClientPolicy(p), sha256.Sum256([]byte(token))}
	return cloneClientPolicy(p), token, nil
}
func (r *ClientRegistry) sweepLocked() {
	for id, e := range r.entries {
		if e.policy.ExpiresAt <= time.Now().UnixMilli() {
			delete(r.entries, id)
		}
	}
}
func (r *ClientRegistry) Authenticate(token string) (ClientPolicy, bool) {
	if r == nil {
		return ClientPolicy{}, false
	}
	hash := sha256.Sum256([]byte(token))
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweepLocked()
	for _, e := range r.entries {
		if subtle.ConstantTimeCompare(hash[:], e.hash[:]) == 1 {
			return cloneClientPolicy(e.policy), true
		}
	}
	return ClientPolicy{}, false
}
func (r *ClientRegistry) Active(id string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweepLocked()
	_, ok := r.entries[id]
	return ok
}
func (r *ClientRegistry) Revoke(id string) {
	if r != nil {
		r.mu.Lock()
		delete(r.entries, id)
		r.mu.Unlock()
	}
}
func (r *ClientRegistry) List() []ClientPolicy {
	out := []ClientPolicy{}
	if r == nil {
		return out
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweepLocked()
	for _, e := range r.entries {
		out = append(out, cloneClientPolicy(e.policy))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Server) authenticateClient(r *http.Request) (*http.Request, error) {
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if s.opts.Token == "" {
		return r.WithContext(WithCaller(r.Context(), "local-owner")), nil
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.opts.Token)) == 1 {
		return r.WithContext(WithCaller(r.Context(), "local-owner")), nil
	}
	if p, ok := s.opts.Clients.Authenticate(token); ok {
		return r.WithContext(context.WithValue(r.Context(), callerKey{}, p)), nil
	}
	return r, errors.New("invalid token: use Authorization: Bearer; query credentials are not accepted")
}

func hasString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// authorizeInvocation is shared by HTTP and MCP. Host/session identifiers are
// immutable policy targets; UI selectors and arbitrary node objects are not.
func (s *Server) authorizeInvocation(ctx context.Context, tool string, args map[string]any) error {
	// Host request structs use encoding/json's case-insensitive field matching.
	// Authorization must not interpret "Local" or "Node" differently from it.
	for key := range args {
		for _, canonical := range []string{"local", "node", "id", "sessionId", "serverId", "includeSecrets", "settings", "token", "confirm", "paths"} {
			if key != canonical && strings.EqualFold(key, canonical) {
				return fmt.Errorf("%w: 安全相关参数必须使用规范字段名 %s", ErrBadInput, canonical)
			}
		}
	}
	if err := rejectSecurityPatch(tool, args); err != nil {
		return err
	}
	if tool == "eval_js" || unsafeUITool(tool) {
		if err := s.allowCap(CapTrustedAutomation, tool); err != nil {
			return err
		}
	}
	if (tool == "open_tab" || tool == OpSessionsConnect) && appctlBool(args, "local") {
		if err := s.allowCap(CapLocalExec, "本机终端"); err != nil {
			return err
		}
	}
	if (tool == "open_tab" || tool == OpSessionsConnect) && !appctlBool(args, "local") {
		if err := s.allowCap(CapCredentialUse, "服务器连接"); err != nil {
			return err
		}
	}
	if tool == OpExecPlan || tool == OpExecBatch {
		return s.authorizeJobBatch(ctx, tool, args)
	}
	p, restricted := ctx.Value(callerKey{}).(ClientPolicy)
	if !restricted || p.ID == "local-owner" {
		return nil
	}
	if !s.opts.Clients.Active(p.ID) {
		return errors.New("客户端授权已撤销或过期")
	}
	if tool == "confirm.prepare" || tool == "confirm.commit" || tool == "confirm.status" {
		return nil
	}
	caps, _ := ToolCapabilities(tool)
	for _, c := range caps {
		if !hasString(p.Capabilities, string(c)) {
			return fmt.Errorf("客户端未获授权能力 %s", c)
		}
	}
	if (tool == "open_tab" || tool == OpSessionsConnect) && !appctlBool(args, "local") && !hasString(p.Capabilities, string(CapCredentialUse)) {
		return fmt.Errorf("客户端未获授权能力 %s", CapCredentialUse)
	}
	if unsafeUITool(tool) || strings.HasPrefix(tool, "ui.") || tool == "eval_js" || tool == "get_settings" || tool == "update_settings" || tool == "settings.reset" || tool == "settings.schema" || tool == "app.command" || tool == "app.commands" || tool == "app_state" || tool == "export_diagnostics" || tool == "recent_events" || tool == "read_logs" || tool == "list_logs" || tool == "app.recent_calls" || tool == "screenshot" || strings.HasPrefix(tool, "credentials.") {
		return errors.New("该工具仅供本机可信所有者使用")
	}
	if appctlBool(args, "includeSecrets") || args["node"] != nil || args["local"] == true {
		return errors.New("受限客户端不允许明文凭据、自定义连接对象或本机执行")
	}
	if !p.AllTargets {
		if err := authorizeClientTarget(p, tool, args); err != nil {
			return err
		}
	}
	if strings.HasPrefix(tool, "sftp.") && tool != "sftp.cancel" && tool != "sftp.transfers" && tool != "sftp.syncCwd" {
		roots := p.ReadPaths
		if tool == OpSftpWrite || tool == OpSftpMkdir || tool == OpSftpRename || tool == OpSftpRemove || tool == "sftp.patch" || tool == "sftp.upload" {
			roots = p.WritePaths
		}
		found := false
		for _, key := range []string{"path", "from", "to", "remotePath"} {
			if raw := appctlString(args, key); raw != "" {
				found = true
				norm, err := NormalizeRemotePath(raw)
				if err != nil || !SFTPPathAllowed(roots, norm) {
					return errors.New("路径不在客户端授权范围内")
				}
			}
		}
		if !found {
			return errors.New("受限文件操作必须指定绝对路径")
		}
	}
	return nil
}

// Targets must come from the exact field consumed by the handler. Looking for
// any supplied serverId/sessionId lets an unrelated allowed ID mask the real
// target, or turn an unfiltered global list into an apparently scoped request.
func authorizeClientTarget(p ClientPolicy, tool string, args map[string]any) error {
	var serverID, sessionID string
	switch tool {
	case "app.health", "app.permissions", OpJobsGet, OpJobsWait, OpJobsOutput, OpJobsCancel, OpJobsList:
		return nil // Jobs enforce immutable owner IDs in their own store.
	case "open_tab", OpSessionsConnect:
		serverID = appctlString(args, "serverId")
	case OpServersGet, OpServersDelete, OpServersDuplicate, OpServersTest:
		serverID = appctlString(args, "id")
	case OpServersMoveGroup:
		ids, ok := appctlStringList(args, "ids")
		if !ok || len(ids) == 0 {
			return errors.New("受限批量操作必须指定获授权的服务器")
		}
		for _, id := range ids {
			if id == "" || !hasString(p.ServerIDs, id) {
				return errors.New("服务器不在客户端授权范围内")
			}
		}
		return nil
	case "read_terminal", "send_input", "scroll_terminal", "resize_terminal", "close_tab", "reconnect_session", "disconnect_session", OpTerminalRun, OpTerminalExpect, OpTerminalSudo:
		sessionID = appctlString(args, "id")
	case OpExecStart, OpSessionsGet, OpSessionsWait, OpSessionsDisconnect, OpSessionsReconnect, OpHostsMetrics, OpProcessesList, OpServicesStatus, OpServicesLogs, OpServicesRestart:
		sessionID = appctlString(args, "sessionId")
	default:
		if IsSftpOp(tool) {
			sessionID = appctlString(args, "sessionId")
			if sessionID == "" {
				sessionID = appctlString(args, "id")
			}
			if tool == OpSftpCancel {
				var err error
				sessionID, _, _, err = ParseTransferID(appctlString(args, "transferId"))
				if err != nil {
					return err
				}
			}
		} else {
			return errors.New("该工具没有安全的目标范围过滤；受限客户端不能调用全局操作")
		}
	}
	if serverID != "" {
		if hasString(p.ServerIDs, serverID) {
			return nil
		}
		return errors.New("服务器不在客户端授权范围内")
	}
	if sessionID != "" && hasString(p.SessionIDs, sessionID) {
		return nil
	}
	return errors.New("会话或服务器不在客户端授权范围内，或缺少精确目标")
}

func unsafeUITool(tool string) bool {
	switch tool {
	case OpUIClick, OpUIType, OpUIKey, OpUIStorePatch, OpUIReload:
		return true
	}
	return false
}

func rejectSecurityPatch(tool string, args map[string]any) error {
	if tool == "capability.enable" {
		return errors.New("权限提升只能由用户在应用内批准；MCP 无权修改自身授权")
	}
	if tool != "update_settings" && tool != "settings.update" && tool != "settings.reset" {
		return nil
	}
	patch := args
	if inner, ok := args["settings"].(map[string]any); ok {
		patch = inner
	}
	for key := range patch {
		key = strings.ToLower(key)
		if key == "debug" || strings.HasPrefix(key, "debug.") {
			return errors.New("调试权限、监听与授权设置只能在应用内修改")
		}
	}
	for _, v := range uiStringList(args["paths"]) {
		v = strings.ToLower(v)
		if v == "debug" || strings.HasPrefix(v, "debug.") {
			return errors.New("不允许通过设置重置修改调试授权")
		}
	}
	return nil
}

type ApprovalRequest struct {
	OperationID string         `json:"operationId"`
	ClientID    string         `json:"clientId"`
	Tool        string         `json:"tool"`
	Action      string         `json:"action"`
	Summary     string         `json:"summary"`
	Args        map[string]any `json:"args"`
	ExpiresAt   int64          `json:"expiresAt"`
}
type ApprovalDecision struct {
	Approved bool
	AllowFor time.Duration
}
type ApprovalFunc func(context.Context, ApprovalRequest) (ApprovalDecision, error)
type approvedOperation struct {
	Caller    string
	Token     string
	ExpiresAt int64
	Consumed  bool
}
type approvalState struct {
	mu         sync.Mutex
	operations map[string]approvedOperation
	grants     map[string]time.Time
}

var approvalStates sync.Map

func (s *Server) approvalState() *approvalState {
	v, _ := approvalStates.LoadOrStore(s, &approvalState{operations: map[string]approvedOperation{}, grants: map[string]time.Time{}})
	return v.(*approvalState)
}
func (s *Server) ClosePolicy() { approvalStates.Delete(s) }
func approvalTarget(tool string, args map[string]any) string {
	for _, k := range []string{"path", "remotePath", "serverId", "sessionId", "id"} {
		if v := appctlString(args, k); v != "" {
			return k + "=" + v
		}
	}
	sum := sha256.Sum256([]byte(NormalizeArgsJSON(args)))
	return hex.EncodeToString(sum[:])
}

func (s *Server) approveOperation(ctx context.Context, action, tool string, args map[string]any) (operationID string, token string, expiresAt int64, resultErr error) {
	epoch := s.policyEpoch.Load()
	args = NormalizeConfirmArgs(args)
	opID := "op_" + newSessionID()
	defer func() {
		if s.opts.Audits != nil {
			s.opts.Audits.Append(AuditRecord{Source: "authorization", Tool: "authorization.approve", ClientID: CallerID(ctx), OperationID: opID, OK: resultErr == nil})
		}
	}()
	if err := ctx.Err(); err != nil {
		return "", "", 0, err
	}
	if s.opts.Confirms == nil {
		return "", "", 0, errors.New("确认管理器未启用")
	}
	if action == "capability.enable" {
		return "", "", 0, rejectSecurityPatch(action, args)
	}
	if tool != "" {
		if err := s.authorizeInvocation(ctx, tool, args); err != nil {
			return "", "", 0, err
		}
		if err := s.checkToolCaps(tool, argDetail(args)); err != nil {
			return "", "", 0, err
		}
	} else if cap, ok := CapabilityOfAction(action); ok {
		if err := s.allowCap(cap, argDetail(args)); err != nil {
			return "", "", 0, err
		}
	}
	if err := rejectSecurityPatch(action, args); err != nil {
		return "", "", 0, err
	}
	if tool == "" && IsRestrictedCaller(ctx) {
		return "", "", 0, errors.New("受限客户端必须提供精确 tool 以核验目标权限")
	}
	state := s.approvalState()
	caller := CallerID(ctx)
	key := caller + "\x00" + action + "\x00" + tool + "\x00" + NormalizeArgsJSON(args)
	state.mu.Lock()
	allowed := state.grants[key].After(time.Now())
	for k, v := range state.grants {
		if !v.After(time.Now()) {
			delete(state.grants, k)
		}
	}
	for k, v := range state.operations {
		if v.ExpiresAt+time.Hour.Milliseconds() < time.Now().UnixMilli() {
			delete(state.operations, k)
		}
	}
	// Keep recent terminal states queryable, but never let already-consumed
	// confirmations prevent a new approval. Active approvals are not evicted.
	if len(state.operations) >= 128 {
		oldestID := ""
		oldestExpiry := int64(0)
		for id, op := range state.operations {
			_, active := s.opts.Confirms.Peek(op.Token)
			if !active && (oldestID == "" || op.ExpiresAt < oldestExpiry) {
				oldestID, oldestExpiry = id, op.ExpiresAt
			}
		}
		if oldestID != "" {
			delete(state.operations, oldestID)
		}
	}
	if len(state.operations) >= 128 {
		state.mu.Unlock()
		return "", "", 0, errors.New("待确认操作已达上限")
	}
	state.mu.Unlock()
	summary := action + "\n" + NormalizeArgsJSON(approvalProjection(args))
	if !allowed {
		if s.opts.Approver == nil {
			return "", "", 0, errors.New("用户审批不可用：服务端未配置可信审批通道，操作未执行")
		}
		approvalCtx, cancel := context.WithTimeout(ctx, DefaultConfirmTTL)
		defer cancel()
		decision, err := s.opts.Approver(approvalCtx, ApprovalRequest{opID, caller, tool, action, summary, safeProjection(args).(map[string]any), time.Now().Add(DefaultConfirmTTL).UnixMilli()})
		if err != nil {
			return "", "", 0, fmt.Errorf("用户审批失败：%w", err)
		}
		if !decision.Approved {
			return "", "", 0, errors.New("用户拒绝了本次操作")
		}
		if approvalCtx.Err() != nil {
			return "", "", 0, errors.New("审批已超时，操作未执行")
		}
		if decision.AllowFor > 0 {
			ttl := decision.AllowFor
			if ttl > 10*time.Minute {
				ttl = 10 * time.Minute
			}
			state.mu.Lock()
			state.grants[key] = time.Now().Add(ttl)
			state.mu.Unlock()
		}
	}
	// Serialize final issuance with invalidation. The epoch also rejects a
	// native approval that was open while permissions or listener settings changed.
	s.policyMu.Lock()
	defer s.policyMu.Unlock()
	if epoch != s.policyEpoch.Load() {
		return "", "", 0, errors.New("审批期间授权设置已变更，请重新准备操作")
	}
	// An identity may have been revoked while the native dialog was open.
	if p, ok := ctx.Value(callerKey{}).(ClientPolicy); ok && p.ID != "local-owner" && !s.opts.Clients.Active(p.ID) {
		return "", "", 0, errors.New("客户端授权已撤销")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.operations) >= 128 {
		return "", "", 0, errors.New("待确认操作已达上限")
	}
	token, expires := s.opts.Confirms.PrepareTool(action, tool, args, summary)
	if token == "" {
		return "", "", 0, errors.New("待确认操作已达上限")
	}
	s.opts.Confirms.BindCaller(token, caller)
	state.operations[opID] = approvedOperation{Caller: caller, Token: token, ExpiresAt: expires}
	return opID, token, expires, nil
}

func (s *Server) checkConfirmCaller(ctx context.Context, token string) error {
	if s.opts.Confirms == nil {
		return nil
	}
	if p, ok := s.opts.Confirms.Peek(token); ok && p.Caller != "" && p.Caller != CallerID(ctx) {
		return errors.New("确认凭据属于其他客户端")
	}
	return nil
}

func (s *Server) confirmationStatus(ctx context.Context, operationID string) (map[string]any, error) {
	state := s.approvalState()
	state.mu.Lock()
	op, ok := state.operations[operationID]
	state.mu.Unlock()
	if !ok || op.Caller != CallerID(ctx) {
		return nil, fmt.Errorf("%w: 未找到本客户端的确认操作", ErrNotFound)
	}
	status := "approved"
	if op.Consumed {
		status = "consumed"
	} else if op.ExpiresAt <= time.Now().UnixMilli() {
		status = "expired"
	} else if s.opts.Confirms == nil {
		status = "consumed"
	} else if _, active := s.opts.Confirms.Peek(op.Token); !active {
		status = "consumed"
	}
	return map[string]any{"operationId": operationID, "status": status, "expiresAt": op.ExpiresAt}, nil
}

func (s *Server) markConfirmationConsumed(token string) {
	state := s.approvalState()
	state.mu.Lock()
	defer state.mu.Unlock()
	for id, op := range state.operations {
		if op.Token == token {
			op.Consumed = true
			state.operations[id] = op
			return
		}
	}
}

// safeProjection strips known secret fields and arbitrary payloads before a
// diagnostic/approval/error response is built. Raw terminal data is omitted by
// the diagnostic producer instead of relying on heuristic redaction.
func safeProjection(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, value := range x {
			lower := strings.ToLower(k)
			if appctlSensitiveArgKey(k) || strings.Contains(lower, "password") || strings.Contains(lower, "passphrase") || strings.Contains(lower, "privatekey") || lower == "base64" || lower == "data" || lower == "command" || lower == "js" || lower == "text" || lower == "output" || lower == "oldtext" || lower == "newtext" || lower == "env" {
				out[k] = "***"
			} else {
				out[k] = safeProjection(value)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = safeProjection(item)
		}
		return out
	default:
		return v
	}
}

func SafeProjection(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]any{"error": "projection unavailable"}
	}
	var normalized any
	if json.Unmarshal(b, &normalized) != nil {
		return nil
	}
	return safeProjection(normalized)
}

// ReadPathScope provides a canonical host-side check for scoped file reads.
func CallerReadPaths(ctx context.Context) []string {
	p, ok := ctx.Value(callerKey{}).(ClientPolicy)
	if !ok || p.ID == "local-owner" {
		return nil
	}
	return append([]string{}, p.ReadPaths...)
}
func CallerWritePaths(ctx context.Context) []string {
	p, ok := ctx.Value(callerKey{}).(ClientPolicy)
	if !ok || p.ID == "local-owner" {
		return nil
	}
	return append([]string{}, p.WritePaths...)
}
func IsRestrictedCaller(ctx context.Context) bool { return CallerID(ctx) != "local-owner" }

func approvalProjection(args map[string]any) map[string]any {
	out := safeProjection(args).(map[string]any)
	for _, key := range []string{"command", "js"} {
		if v, ok := args[key].(string); ok {
			out[key] = v
		}
	}
	for _, key := range []string{"content", "base64", "oldText", "newText", "env"} {
		if value, ok := args[key]; ok {
			data, _ := json.Marshal(value)
			if text, ok := value.(string); ok {
				data = []byte(text)
			}
			sum := sha256.Sum256(data)
			out[key] = map[string]any{"bytes": len(data), "sha256": hex.EncodeToString(sum[:]), "content": "***"}
		}
	}
	return out
}
func (s *Server) IssueClient(p ClientPolicy) (map[string]any, error) {
	if s.opts.Clients == nil {
		return nil, errors.New("客户端注册表不可用")
	}
	policy, token, err := s.opts.Clients.Issue(p)
	if err != nil {
		return nil, err
	}
	if s.opts.Audits != nil {
		s.opts.Audits.Append(AuditRecord{Source: "authorization", Tool: "authorization.issue", ClientID: policy.ID, OperationID: "op_" + newSessionID(), OK: true})
	}
	return map[string]any{"client": policy, "token": token, "note": "token 只显示一次，客户端重启后需重新授权"}, nil
}
func (s *Server) ListClients() []ClientPolicy { return s.opts.Clients.List() }
func (s *Server) RevokeClient(id string) {
	s.opts.Clients.Revoke(id)
	s.CancelClientJobs(id)
	state := s.approvalState()
	state.mu.Lock()
	for key, op := range state.operations {
		if op.Caller == id {
			delete(state.operations, key)
		}
	}
	for key := range state.grants {
		if strings.HasPrefix(key, id+"\x00") {
			delete(state.grants, key)
		}
	}
	state.mu.Unlock()
}
func (s *Server) authorizeEvent(ctx context.Context, ev Event) bool {
	switch ev.Topic {
	case "jobs.changed":
		if s.allowCap(CapTerminalRead, ev.SessionID) != nil {
			return false
		}
		var payload struct {
			JobID string `json:"jobId"`
		}
		if json.Unmarshal(ev.Data, &payload) != nil || payload.JobID == "" {
			return false
		}
		m := s.jobStore()
		m.mu.Lock()
		j := m.jobs[payload.JobID]
		owned := j != nil && j.view.Owner == CallerID(ctx) && j.view.SessionID == ev.SessionID
		m.mu.Unlock()
		if !owned {
			return false
		}
		if IsRestrictedCaller(ctx) {
			return ev.SessionID != "" && s.authorizeInvocation(ctx, "read_terminal", map[string]any{"id": ev.SessionID}) == nil
		}
		return true
	case TerminalOutputTopic, "ssh.status", "ssh.progress":
		if err := s.allowCap(CapTerminalRead, ev.SessionID); err != nil {
			return false
		}
		if IsRestrictedCaller(ctx) {
			return ev.SessionID != "" && s.authorizeInvocation(ctx, "read_terminal", map[string]any{"id": ev.SessionID}) == nil
		}
		return true
	case "sftp.transfer", "sftp.dir-updated", "sftp.sync-path":
		if s.allowCap(CapRemoteFSRead, ev.SessionID) != nil {
			return false
		}
		if IsRestrictedCaller(ctx) {
			return false
		}
		return true
	default:
		if IsRestrictedCaller(ctx) {
			return false
		}
		if strings.HasPrefix(ev.Topic, "ui.") {
			return s.allowCap(CapUIInspect, "") == nil
		}
		return true
	}
}
func cloneClientPolicy(p ClientPolicy) ClientPolicy {
	p.Capabilities = append([]string(nil), p.Capabilities...)
	p.ServerIDs = append([]string(nil), p.ServerIDs...)
	p.SessionIDs = append([]string(nil), p.SessionIDs...)
	p.ReadPaths = append([]string(nil), p.ReadPaths...)
	p.WritePaths = append([]string(nil), p.WritePaths...)
	return p
}
func (s *Server) InvalidateAuthorizations() {
	s.policyMu.Lock()
	defer s.policyMu.Unlock()
	s.policyEpoch.Add(1)
	s.ClosePolicy()
	if s.opts.Confirms != nil {
		s.opts.Confirms.Clear()
	}
}
