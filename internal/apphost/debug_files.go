package apphost

import (
	"context"
	"ding-ssh/internal/debugsrv"
	"ding-ssh/internal/sshx"
	"github.com/pkg/sftp"
	"path"
)

func ctlScopedWritePath(ctx context.Context, client *sftp.Client, target string, finalLink bool) error {
	if !debugsrv.IsRestrictedCaller(ctx) {
		return nil
	}
	return sshx.ValidateRemoteWritePath(client, debugsrv.CallerWritePaths(ctx), target, finalLink)
}
func ctlScopedReadPath(ctx context.Context, client *sftp.Client, target string) error {
	if !debugsrv.IsRestrictedCaller(ctx) {
		return nil
	}
	return sshx.ValidateRemoteReadPath(client, debugsrv.CallerReadPaths(ctx), target)
}

func (c *debugController) sftpHostPatch(ctx context.Context, args map[string]any) (any, error) {
	client, sess, err := c.ctlSftpClient(args)
	if err != nil {
		return nil, err
	}
	target, err := debugsrv.NormalizeRemotePath(ctlArgStr(args, "path"))
	if err != nil {
		return nil, err
	}
	if err := ctlScopedReadPath(ctx, client, target); err != nil {
		return nil, err
	}
	if err := (debugGate{c}).Allow(debugsrv.CapRemoteFSRead, target); err != nil {
		return nil, err
	}
	dry := ctlArgBool(args, "dryRun")
	if !dry {
		if err := ctlScopedWritePath(ctx, client, target, false); err != nil {
			return nil, err
		}
		if _, err := c.ctlRequireSftpWritePaths(target); err != nil {
			return nil, err
		}
		if err := sshx.ValidateRemoteWritePath(client, c.app.currentDebugSettings().SFTPWriteAllowlist, target, false); err != nil {
			return nil, err
		}
	}
	oldText, _ := args["oldText"].(string)
	newText, _ := args["newText"].(string)
	count := ctlArgInt(args, "expectedMatches")
	if count == 0 {
		count = 1
	}
	result, err := sshx.PatchRemoteFile(ctx, client, target, oldText, newText, count, ctlArgStr(args, "expectedHash"), dry, c.app.currentDebugSettings().SFTPWriteAllowlist)
	if err != nil {
		return nil, err
	}
	if result.Applied {
		c.app.manager.InvalidateSftpCache(path.Dir(target))
	}
	return map[string]any{"sessionId": sess, "path": target, "expectedHash": result.ExpectedHash, "newHash": result.NewHash, "matches": result.Matches, "beforeBytes": result.BeforeBytes, "afterBytes": result.AfterBytes, "changed": result.Changed, "dryRun": result.DryRun, "applied": result.Applied, "atomic": result.Atomic}, nil
}
