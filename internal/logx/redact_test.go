package logx

import (
	"strings"
	"testing"
)

// URL 查询串与 key=value 形式的 token 必须被替换，其余参数保持可读。
func TestRedactToken(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "查询串 token",
			in:   "[api] GET /v1/state?token=abc123&limit=10 200 3ms",
			want: "[api] GET /v1/state?token=***&limit=10 200 3ms",
		},
		{
			name: "access_token 赋值",
			in:   "refresh access_token=zzz-999&scope=all",
			want: "refresh access_token=***&scope=all",
		},
		{
			name: "token 冒号形式",
			in:   "token: 8f3c2b",
			want: "token: ***",
		},
	}
	for _, c := range cases {
		if got := Redact(c.in); got != c.want {
			t.Fatalf("%s: Redact(%q) = %q，期望 %q", c.name, c.in, got, c.want)
		}
	}
}

// JSON 字段只替换值，键名保留，便于排查时看清是哪个字段。
func TestRedactJSONSecrets(t *testing.T) {
	in := `{"user":"root","password":"hunter2","keyContent":"-----BEGINx","port":22}`
	got := Redact(in)
	if !strings.Contains(got, `"password":"***"`) {
		t.Fatalf("password 未被脱敏: %s", got)
	}
	if !strings.Contains(got, `"keyContent":"***"`) {
		t.Fatalf("keyContent 未被脱敏: %s", got)
	}
	if !strings.Contains(got, `"user":"root"`) || !strings.Contains(got, `"port":22`) {
		t.Fatalf("非敏感字段被破坏: %s", got)
	}
	if strings.Contains(got, "hunter2") {
		t.Fatalf("明文密码仍在日志里: %s", got)
	}
}

// Authorization: Bearer 凭据必须被替换（大小写不敏感）。
func TestRedactAuthorizationBearer(t *testing.T) {
	in := "POST /mcp Authorization: Bearer 9f8e7d6c5b4a status=401"
	got := Redact(in)
	if got != "POST /mcp Authorization: Bearer *** status=401" {
		t.Fatalf("Bearer 脱敏结果 = %q", got)
	}

	lower := "authorization: bearer TOPSECRET"
	if got := Redact(lower); strings.Contains(got, "TOPSECRET") {
		t.Fatalf("小写 authorization 未脱敏: %q", got)
	}
}

// PEM 私钥整段（跨行）与只有 BEGIN 行的截断情况都要被替换。
func TestRedactPrivateKey(t *testing.T) {
	block := "写入配置 -----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\nAAAABGNoaXBy\n-----END OPENSSH PRIVATE KEY----- 完成"
	got := Redact(block)
	if strings.Contains(got, "b3BlbnNzaC1rZXktdjE") {
		t.Fatalf("私钥内容仍在日志里: %q", got)
	}
	if !strings.HasPrefix(got, "写入配置 ***") || !strings.HasSuffix(got, "完成") {
		t.Fatalf("私钥脱敏破坏了上下文: %q", got)
	}

	truncated := "key=-----BEGIN RSA PRIVATE KEY-----"
	if got := Redact(truncated); strings.Contains(got, "RSA PRIVATE KEY-----") {
		t.Fatalf("截断的私钥头未脱敏: %q", got)
	}
}

// 不含凭据的日志（含中文与普通单词）必须原样返回，避免误伤可读性。
func TestRedactKeepsPlainText(t *testing.T) {
	for _, s := range []string{
		"",
		"[api] GET /v1/health 200 0ms 87b",
		"终端输出：total 12 files",
		"tokenizer 已初始化，token 数量 12",
		"ssh:output:tab-1 收到 4096 字节",
	} {
		if got := Redact(s); got != s {
			t.Fatalf("普通日志被改动: Redact(%q) = %q", s, got)
		}
	}
}
