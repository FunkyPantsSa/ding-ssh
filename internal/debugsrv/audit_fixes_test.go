package debugsrv

import (
	"context"
	"testing"
)

func TestStoreSecretGateBeforeFrontend(t *testing.T) {
	s := &Server{}
	for _, path := range []string{"", "servers.0.password", "list.0.content"} {
		fe := &stubUI{}
		if _, err := s.uiStore(context.Background(), fe, map[string]any{"name": "servers", "path": path}); err == nil {
			t.Fatal("secret store must be gated")
		}
		if len(fe.calls) != 0 {
			t.Fatal("must not fetch frontend state before authorization")
		}
	}
	fe := &stubUI{}
	if _, err := s.uiStore(context.Background(), fe, map[string]any{}); err != nil {
		t.Fatal(err)
	}
}

func TestSFTPPageArguments(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.h.replies[OpSftpList] = SFTPListPayload{Path: "/", Entries: []SFTPEntryInfo{{Name: "a"}, {Name: "b"}, {Name: "c"}}}
	out := env.callJSON(t, OpSftpList, map[string]any{"sessionId": "s1", "offset": 10, "limit": 2})
	if out["nextOffset"] != float64(12) || out["truncated"] != true {
		t.Fatalf("unexpected page: %v", out)
	}
	a := env.h.lastArgsFor(OpSftpList)
	if a["offset"] != float64(10) || a["limit"] != float64(3) {
		t.Fatalf("host paging lost: %v", a)
	}
	env.callErr(t, OpSftpList, map[string]any{"sessionId": "s1", "offset": -1})
	env.callErr(t, OpSftpList, map[string]any{"sessionId": "s1", "limit": 501})
}

func TestToolBoundConfirmationDispatch(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	// A permission failure must leave the confirmation available for retry.
	token, _ := env.srv.opts.Confirms.PrepareTool("sftp.write", OpSftpSyncCwd, map[string]any{"sessionId": "s1", "remotePath": "/not-allowed"}, "sync")
	if _, err := env.srv.confirmCommit(context.Background(), map[string]any{"token": token}); err == nil {
		t.Fatal("expected permission/preflight refusal")
	}
	if _, ok := env.srv.opts.Confirms.Peek(token); !ok {
		t.Fatal("preflight failure consumed token")
	}
}

func TestCommitExecutesBoundToolAndRejectsReuse(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.scope.set("/srv")
	env.h.replies[OpSftpSyncCwd] = map[string]any{"ok": true}
	args := map[string]any{"sessionId": "s1", "remotePath": "/srv"}
	token, _ := env.srv.opts.Confirms.PrepareTool("sftp.write", OpSftpSyncCwd, args, "sync")
	wrong := map[string]any{"sessionId": "s1", "path": "/srv/file", "content": "x", "token": token}
	if _, _, err := env.srv.mcpCallToolAudited(context.Background(), OpSftpWrite, wrong); err == nil {
		t.Fatal("bound token accepted by another tool")
	}
	if _, err := env.srv.confirmCommit(context.Background(), map[string]any{"token": token}); err != nil {
		t.Fatal(err)
	}
	if env.h.lastArgsFor(OpSftpSyncCwd) == nil {
		t.Fatal("did not dispatch original tool")
	}
	if env.h.lastArgsFor(OpSftpWrite) != nil {
		t.Fatal("dispatched shared action instead of tool")
	}
	if _, err := env.srv.confirmCommit(context.Background(), map[string]any{"token": token}); err == nil {
		t.Fatal("replayed confirmation")
	}
}

func TestUnsupportedActionsAreNotAdvertised(t *testing.T) {
	for _, action := range ConfirmableActions() {
		if action == "app.reload" || action == "sftp.upload" {
			t.Fatalf("unsupported action advertised: %s", action)
		}
	}
}

func TestTransferTasksSurviveTerminalTrafficAndSameName(t *testing.T) {
	env := newSFTPEnv(t)
	env.hub.PublishRaw("sftp:transfer:s", map[string]any{"sessionId": "s", "direction": "upload", "name": "same.txt", "transferId": "first", "transferred": 4, "done": true})
	env.hub.PublishRaw("sftp:transfer:s", map[string]any{"sessionId": "s", "direction": "upload", "name": "same.txt", "transferId": "second", "transferred": 2, "done": false})
	for i := 0; i < hubRecentLimit+1; i++ {
		env.hub.Publish(Event{Topic: "ssh.output", SessionID: "s"})
	}
	tasks := env.srv.transferSnapshots("s")
	if len(tasks) != 2 {
		t.Fatalf("tasks merged or evicted: %v", tasks)
	}
	if tasks[0].ID == tasks[1].ID {
		t.Fatal("same-name task IDs merged")
	}
	for _, task := range tasks {
		sess, direction, name, err := ParseTransferID(task.ID)
		if err != nil || sess != "s" || direction != "upload" || (name != "@id:first" && name != "@id:second") {
			t.Fatalf("cannot cancel task precisely: %v %v", task, err)
		}
	}
}
