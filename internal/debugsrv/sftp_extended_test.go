package debugsrv

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestSFTPBinaryTransferAndVersionForwarding(t *testing.T) {
	env := newSFTPEnv(t)
	env.scope.set("/srv/app")
	payload := []byte{0, 0xff, 0x80, '\n', 42}
	env.h.replies[OpSftpWrite] = map[string]any{"path": "/srv/app/bin", "atomic": true, "sha256": strings.Repeat("1", 64)}
	args := map[string]any{"sessionId": "s1", "path": "/srv/app/bin", "base64": base64.StdEncoding.EncodeToString(payload), "expectedHash": "missing"}
	out := env.callJSON(t, OpSftpUpload, env.withToken(t, OpSftpUpload, args))
	if out["atomic"] != true {
		t.Fatalf("missing atomic result: %v", out)
	}
	sent := env.h.lastArgsFor(OpSftpWrite)
	if sent["expectedHash"] != "missing" || sent["base64"] != args["base64"] {
		t.Fatalf("corrupted binary/version: %v", sent)
	}
	env.h.replies[OpSftpRead] = SFTPReadPayload{Path: "/srv/app/bin", Data: payload, Size: 10, EOF: false}
	out = env.callJSON(t, OpSftpDownload, map[string]any{"sessionId": "s1", "path": "/srv/app/bin", "offset": 2})
	if out["base64"] != args["base64"] || out["nextOffset"] != float64(7) || out["truncated"] != true {
		t.Fatalf("bad download page: %v", out)
	}
}

func TestSFTPPatchPreviewAndCommitAreDistinct(t *testing.T) {
	env := newSFTPEnv(t)
	env.scope.set("/srv/app")
	env.h.replies[OpSftpPatch] = map[string]any{"expectedHash": strings.Repeat("1", 64), "dryRun": true, "applied": false}
	args := map[string]any{"sessionId": "s1", "path": "/srv/app/config", "oldText": "old", "newText": "new"}
	env.callJSON(t, OpSftpPatchPreview, args)
	sent := env.h.lastArgsFor(OpSftpPatch)
	if sent["dryRun"] != true {
		t.Fatal("preview wrote file")
	}
	before := env.h.count()
	env.callErr(t, OpSftpPatch, args)
	if env.h.count() != before {
		t.Fatal("unapproved patch reached host")
	}
	args["expectedHash"] = strings.Repeat("1", 64)
	env.callJSON(t, OpSftpPatch, env.withToken(t, OpSftpPatch, args))
	sent = env.h.lastArgsFor(OpSftpPatch)
	if sent["dryRun"] != false || sent["expectedHash"] != args["expectedHash"] {
		t.Fatalf("bad commit args: %v", sent)
	}
}

func TestSFTPReadGateAndNavigationWithoutWriteAllowlist(t *testing.T) {
	env := newSFTPEnv(t)
	env.srv.opts.Gate = &fakeGate{denied: map[Capability]string{CapRemoteFSRead: "file read denied"}}
	if text := env.callErr(t, OpSftpDownload, map[string]any{"sessionId": "s1", "path": "/srv/app/file"}); !strings.Contains(text, "denied") {
		t.Fatal(text)
	}
	if env.h.count() != 0 {
		t.Fatal("denied read reached host")
	}
	env.h.replies[OpSftpSyncCwd] = SFTPMutationPayload{OK: true, Path: "/read-only"}
	env.callJSON(t, OpSftpSyncCwd, map[string]any{"sessionId": "s1", "remotePath": "/read-only"})
}
