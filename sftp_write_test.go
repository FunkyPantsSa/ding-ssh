package main

// M9 收尾：sftp.write 的「写入内容」解释规则回归测试。
//
// 缺陷：宿主原先用「base64 的**值**是否为空」判断有没有内容，于是
//   - debugsrv 的 sftpWrite 总是把内容编码成 base64 传下来，空内容编码后正好是空串；
//   - 于是 sftp.write{content:""}（sftpWriteContent 的中文提示里明确支持的「写空文件」写法）
//     被误判成「缺少写入内容」，报错与真实原因不符，也永远创建不出 0 字节文件。
//
// 修法：判定依据改成**键是否存在**（base64 优先，其次 content），空字符串是合法内容。
// 这里直接测这个纯函数，不需要 SSH / 真实远端。

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestSftpWriteContentResolution(t *testing.T) {
	cases := []struct {
		name    string
		args    map[string]any
		wantB64 string
		wantErr string
	}{
		{
			name:    "base64 键在且为空 = 写 0 字节（不许报缺少内容）",
			args:    map[string]any{"base64": ""},
			wantB64: "",
		},
		{
			name:    "content 键在且为空 = 写 0 字节",
			args:    map[string]any{"content": ""},
			wantB64: "",
		},
		{
			name:    "两键都没给 = 报缺少内容",
			args:    map[string]any{"path": "/tmp/x"},
			wantErr: "缺少写入内容",
		},
		{
			name:    "content 按 text 编码",
			args:    map[string]any{"content": "hello"},
			wantB64: base64.StdEncoding.EncodeToString([]byte("hello")),
		},
		{
			name:    "content + encoding=base64 原样透传",
			args:    map[string]any{"content": base64.StdEncoding.EncodeToString([]byte("hi")), "encoding": "base64"},
			wantB64: base64.StdEncoding.EncodeToString([]byte("hi")),
		},
		{
			name:    "content 不是合法 base64（encoding=base64）要报错",
			args:    map[string]any{"content": "!!!", "encoding": "base64"},
			wantErr: "不是合法 base64",
		},
		{
			name:    "base64 必须是字符串",
			args:    map[string]any{"base64": 12},
			wantErr: "必须是字符串",
		},
		{
			name:    "content 必须是字符串",
			args:    map[string]any{"content": 12},
			wantErr: "必须是字符串",
		},
		{
			name:    "base64 优先于 content",
			args:    map[string]any{"base64": base64.StdEncoding.EncodeToString([]byte("A")), "content": "B"},
			wantB64: base64.StdEncoding.EncodeToString([]byte("A")),
		},
		{
			name:    "base64 两端空白被裁剪",
			args:    map[string]any{"base64": "  " + base64.StdEncoding.EncodeToString([]byte("C")) + "\n"},
			wantB64: base64.StdEncoding.EncodeToString([]byte("C")),
		},
	}
	for _, tc := range cases {
		got, err := ctlSftpWriteContentB64(tc.args)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("%s：期望含 %q 的错误，实际 err=%v", tc.name, tc.wantErr, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s：不应报错，实际 %v", tc.name, err)
		}
		if got != tc.wantB64 {
			t.Fatalf("%s：base64 = %q，期望 %q", tc.name, got, tc.wantB64)
		}
	}
}
