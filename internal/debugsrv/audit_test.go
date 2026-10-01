package debugsrv

import (
	"encoding/json"
	"strings"
	"testing"
)

// 审计记录必须脱敏：明文密钥 / token 一律不能落进缓冲。
func TestAuditRedactsSecrets(t *testing.T) {
	a := NewAuditLog()
	rec := a.Append(AuditRecord{
		Source: "mcp",
		Tool:   "credentials.create",
		Cap:    string(CapSecretsWrite),
		Args:   `{"name":"prod","password":"p@ssw0rd","keyContent":"-----BEGIN OPENSSH PRIVATE KEY-----","token":"tok-123"}`,
		OK:     true,
	})
	if strings.Contains(rec.Args, "p@ssw0rd") || strings.Contains(rec.Args, "tok-123") {
		t.Fatalf("审计参数泄露了明文密钥：%s", rec.Args)
	}
	if !strings.Contains(rec.Args, "***") {
		t.Fatalf("审计参数应出现打码占位符：%s", rec.Args)
	}
	// 非敏感字段要保留（审计的可读性同样重要）
	if !strings.Contains(rec.Args, "prod") {
		t.Fatalf("非敏感字段应保留：%s", rec.Args)
	}
}

// 各种「密钥出现方式」都要打码：JSON、key=value、转义引号、Bearer、PEM。
func TestRedactArgsJSONVariants(t *testing.T) {
	cases := []struct {
		name string
		in   string
		leak []string
	}{
		{"json 明文", `{"password":"hunter2"}`, []string{"hunter2"}},
		{"key=value", `token=abc123`, []string{"abc123"}},
		{"转义引号", `{\"password\":\"hunter2\"}`, []string{"hunter2"}},
		{"Bearer", `Authorization: Bearer xyz.abc.def`, []string{"xyz.abc.def"}},
		{"PEM 私钥", "-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----", []string{"MIIEow"}},
		{"keyContent", `{"keyContent":"AAAAB3NzaC1yc2E"}`, []string{"AAAAB3NzaC1yc2E"}},
	}
	for _, tc := range cases {
		got := RedactArgsJSON(tc.in)
		for _, leak := range tc.leak {
			if strings.Contains(got, leak) {
				t.Fatalf("%s：脱敏后仍含 %q → %s", tc.name, leak, got)
			}
		}
	}
	// 幂等：已脱敏的文本再脱敏不会变成 ***
	once := RedactArgsJSON(`{"password":"hunter2"}`)
	if twice := RedactArgsJSON(once); twice != once {
		t.Fatalf("脱敏应幂等：%s != %s", twice, once)
	}
}

// 环形缓冲上限：只保留最近 auditRingLimit 条，且最新在前。
func TestAuditRingLimit(t *testing.T) {
	a := NewAuditLog()
	for i := 0; i < AuditRingLimit+50; i++ {
		a.Append(AuditRecord{Source: "mcp", Tool: "read_terminal", Args: `{"i":` + itoa(i) + `}`})
	}
	if a.Len() != AuditRingLimit {
		t.Fatalf("环形缓冲长度 = %d，期望 %d", a.Len(), AuditRingLimit)
	}
	// 最新在前：最后一条的 args 里应是最大的 i
	recent := a.Recent(1, "", "")
	if len(recent) != 1 {
		t.Fatalf("Recent(1) 应返回 1 条，实际 %d", len(recent))
	}
	want := itoa(AuditRingLimit + 49)
	if !strings.Contains(recent[0].Args, want) {
		t.Fatalf("最新记录应为 i=%s，实际 %s", want, recent[0].Args)
	}
	// ID 单调递增且唯一
	if recent[0].ID == "" {
		t.Fatalf("记录应带自增 ID")
	}
}

// 默认条数与上限：Recent(0) 取 50；请求超过环形容量时按容量截断。
func TestAuditRecentLimit(t *testing.T) {
	a := NewAuditLog()
	for i := 0; i < 80; i++ {
		a.Append(AuditRecord{Source: "mcp", Tool: "list_terminals"})
	}
	if got := len(a.Recent(0, "", "")); got != 50 {
		t.Fatalf("Recent(0) 应默认 50 条，实际 %d", got)
	}
	if got := len(a.Recent(1000, "", "")); got != 80 {
		t.Fatalf("Recent(1000) 应返回全部 80 条，实际 %d", got)
	}
}

// 按能力位 / 来源过滤。
func TestAuditFilter(t *testing.T) {
	a := NewAuditLog()
	a.Append(AuditRecord{Source: "mcp", Tool: "update_settings", Cap: string(CapConfigWrite), OK: true})
	a.Append(AuditRecord{Source: "http", Tool: "update_settings", Cap: string(CapConfigWrite), OK: false, Error: "被拒"})
	a.Append(AuditRecord{Source: "mcp", Tool: "send_input", Cap: string(CapTerminalInput), OK: true})
	a.Append(AuditRecord{Source: "ui", Tool: "capability.enable", Cap: string(CapRemoteFSWrite), OK: true})

	if got := a.Recent(10, string(CapConfigWrite), ""); len(got) != 2 {
		t.Fatalf("按能力过滤应得 2 条，实际 %d", len(got))
	}
	if got := a.Recent(10, "", "mcp"); len(got) != 2 {
		t.Fatalf("按来源过滤应得 2 条，实际 %d", len(got))
	}
	if got := a.Recent(10, string(CapConfigWrite), "http"); len(got) != 1 || got[0].OK {
		t.Fatalf("组合过滤应得 1 条失败记录，实际 %+v", got)
	}
	if got := a.Recent(10, "", "ui"); len(got) != 1 || got[0].Tool != "capability.enable" {
		t.Fatalf("ui 来源应只有能力开关记录，实际 %+v", got)
	}
}

// 审计事件：每条记录都要通过 Hub 发一条 debug.audit（前端实时刷新依赖它）。
func TestAuditPublishesEvent(t *testing.T) {
	hub := NewHub()
	id, ch := hub.Subscribe(AuditTopic)
	defer hub.Unsubscribe(id)

	a := NewAuditLog()
	rec := a.Append(AuditRecord{Source: "mcp", Tool: "clear_logs", Cap: string(CapLifecycle), OK: true})
	publishAudit(hub, rec)

	select {
	case ev := <-ch:
		if ev.Topic != AuditTopic {
			t.Fatalf("事件 topic = %q，期望 %q", ev.Topic, AuditTopic)
		}
		var got AuditRecord
		if err := json.Unmarshal(ev.Data, &got); err != nil {
			t.Fatalf("事件 data 应是审计记录 JSON：%v", err)
		}
		if got.Tool != "clear_logs" || !got.OK {
			t.Fatalf("事件内容不正确：%+v", got)
		}
	default:
		t.Fatalf("应发布一条 debug.audit 事件")
	}
	// 没有工具的记录（例如中间件兜底失败）不应发布事件
	publishAudit(hub, AuditRecord{})
	select {
	case ev := <-ch:
		t.Fatalf("空记录不应发布事件：%+v", ev)
	default:
	}
}

// 工具调用审计必须记录耗时（>=0）与来源。
func TestAuditRecordFields(t *testing.T) {
	ts := newTestServer(t, &fakeGate{}, &fakeExecutor{})
	_, _, _ = callTool(t, ts.srv, "list_terminals", nil)
	rec := ts.lastAudit()
	if rec.Source != "mcp" {
		t.Fatalf("来源应为 mcp，实际 %q", rec.Source)
	}
	if rec.DurationMS < 0 {
		t.Fatalf("耗时不应为负：%d", rec.DurationMS)
	}
	if rec.TS == 0 {
		t.Fatalf("应带时间戳")
	}
	if rec.Args == "" {
		t.Fatalf("args 至少应为 {}")
	}
}

// 参数里带 token 时，审计记录里也不应出现明文 token。
func TestAuditToolArgsRedacted(t *testing.T) {
	ts := newTestServer(t, &fakeGate{}, &fakeExecutor{})
	_, _, _ = callTool(t, ts.srv, "confirm.prepare", map[string]any{
		"action":  "logs.clear",
		"args":    map[string]any{"password": "top-secret"},
		"summary": "测试",
	})
	rec := ts.lastAudit()
	if strings.Contains(rec.Args, "top-secret") {
		t.Fatalf("审计参数泄露了密钥：%s", rec.Args)
	}
}

// itoa 避免引 strconv（测试里只用得到正整数）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
