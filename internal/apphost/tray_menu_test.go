package apphost

import (
	"testing"

	"ding-ssh/internal/models"
	"ding-ssh/internal/sshx"
)

func TestFormatTraySessionLabel(t *testing.T) {
	cases := []struct {
		name string
		info models.SessionInfo
		want string
	}{
		{
			name: "named server with endpoint",
			info: models.SessionInfo{SessionID: "s1", ServerName: "生产库", Host: "10.0.0.8", User: "ops"},
			want: "生产库 (ops@10.0.0.8)",
		},
		{
			name: "server name equals endpoint",
			info: models.SessionInfo{SessionID: "s2", ServerName: "root@10.0.0.9", Host: "10.0.0.9", User: "root"},
			want: "root@10.0.0.9",
		},
		{
			name: "unnamed server falls back to endpoint",
			info: models.SessionInfo{SessionID: "s3", Host: "10.0.0.10", User: "ubuntu"},
			want: "ubuntu@10.0.0.10",
		},
		{
			name: "session id fallback",
			info: models.SessionInfo{SessionID: "s4"},
			want: "s4",
		},
	}
	for _, tc := range cases {
		if got := formatTraySessionLabel(tc.info); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestConnectedSSHSessionsEmptyAndSafeWithoutDesktop(t *testing.T) {
	app := &App{manager: sshx.NewManager(nil)}
	if got := app.connectedSSHSessions(); len(got) != 0 {
		t.Fatalf("expected no connected sessions, got %#v", got)
	}
	// Should be a safe no-op when desktop runtime isn't attached.
	app.refreshTrayMenu()
	app.openSSHSessionWindow("missing")
}
