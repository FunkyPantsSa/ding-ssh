package localterm

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"ding-ssh/internal/models"
)

func TestWindowsRealPTYLifecycle(t *testing.T) {
	for _, shell := range []string{"powershell", "cmd"} {
		t.Run(shell, func(t *testing.T) {
			var mu sync.Mutex
			var output string
			m := NewManager(func(string, interface{}) {})
			m.notify = func(_ string, payload interface{}) {
				if e, ok := payload.(models.OutputEvent); ok {
					data, _ := decodeBase64(e.Data)
					mu.Lock()
					output += string(data)
					mu.Unlock()
				}
			}
			t.Cleanup(m.CloseAll)
			wait := func(label string, ready func() bool) {
				t.Helper()
				deadline := time.Now().Add(15 * time.Second)
				for !ready() {
					if time.Now().After(deadline) {
						mu.Lock()
						got := output
						mu.Unlock()
						t.Fatalf("%s timed out; output=%q", label, got)
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
			if err := m.Connect("windows-native", shell, 80, 24); err != nil {
				t.Fatal(err)
			}
			// Wait for shell initialization so PSReadLine does not consume the
			// beginning of the command as console startup input.
			wait("shell prompt", func() bool {
				mu.Lock()
				defer mu.Unlock()
				return strings.Contains(output, ">")
			})
			command, expected := "echo DING_%PROCESSOR_ARCHITECTURE%\r", "DING_"+os.Getenv("PROCESSOR_ARCHITECTURE")
			if shell == "powershell" {
				command, expected = "Write-Output ('DING_' + [char]0x4E2D + [char]0x6587)\r", "DING_中文"
			}
			if err := m.WriteRaw("windows-native", []byte(command)); err != nil {
				t.Fatal(err)
			}
			wait("real command output", func() bool {
				mu.Lock()
				defer mu.Unlock()
				return strings.Contains(output, expected)
			})
			if err := m.Resize("windows-native", 100, 30); err != nil {
				t.Fatal(err)
			}
			if err := m.WriteRaw("windows-native", []byte("exit\r")); err != nil {
				t.Fatal(err)
			}
			wait("shell exit cleanup", func() bool { return !m.Has("windows-native") })
			if err := m.Connect("windows-native", shell, 0, 0); err != nil {
				t.Fatal(err)
			}
			if err := m.Disconnect("windows-native"); err != nil {
				t.Fatal(err)
			}
			if m.Has("windows-native") {
				t.Fatal("disconnected session retained")
			}
		})
	}
}
