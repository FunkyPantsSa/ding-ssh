package sshx

import (
	"encoding/base64"

	"ding-ssh/internal/logx"
)

func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func decodeBase64(s string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		// 只记编码与长度，绝不记内容（可能包含终端输入）。
		logx.Debugf("[sshx] base64 解码失败: encoding=std length=%d err=%v", len(s), err)
		return nil, err
	}
	return data, nil
}
