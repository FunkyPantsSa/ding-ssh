package debugsrv

import (
	"context"
	"fmt"
)

func sftpPatchSchema(write bool) map[string]any {
	props := map[string]any{
		"sessionId": strProp("SSH 会话 id"), "path": strProp("远端绝对路径"),
		"oldText": strProp("要精确匹配的非空 UTF-8 文本，不忽略空白"), "newText": strProp("替换文本，可为空"),
		"expectedMatches": intProp("必须匹配的次数，默认 1，范围 1–1000；不匹配即拒绝"),
	}
	if write {
		props["expectedHash"] = strProp("预览返回的整个文件 SHA-256，提交时必填")
		props["dryRun"] = boolProp("仅预览，不修改文件")
		props["confirm"] = strProp("已批准操作的确认 token")
	}
	return obj(props, "sessionId", "path", "oldText", "newText")
}

func (s *Server) sftpPatch(ctx context.Context, name string, args map[string]any) (any, error) {
	sess, err := sftpSessionArg(args)
	if err != nil {
		return nil, err
	}
	oldText, oldOK := args["oldText"].(string)
	newText, newOK := args["newText"].(string)
	if !oldOK || oldText == "" || !newOK {
		return nil, fmt.Errorf("%w: oldText 必须是非空字符串，newText 必须是字符串", ErrBadInput)
	}
	if len(oldText) > sftpWriteMaxBytes || len(newText) > sftpWriteMaxBytes {
		return nil, fmt.Errorf("%w: 替换文本超过 1 MiB", ErrBadInput)
	}
	count, err := sftpIntArg(args, "expectedMatches", 1, 1, 1000)
	if err != nil {
		return nil, err
	}
	dry := name == OpSftpPatchPreview || appctlBool(args, "dryRun")
	p, err := NormalizeRemotePath(appctlString(args, "path"))
	if err != nil {
		return nil, err
	}
	if !dry {
		if _, err := s.sftpRequireWritePath("path", p); err != nil {
			return nil, err
		}
	}
	return s.callSFTPHost(ctx, OpSftpPatch, map[string]any{"sessionId": sess, "path": p, "oldText": oldText, "newText": newText, "expectedMatches": count, "expectedHash": appctlString(args, "expectedHash"), "dryRun": dry})
}
