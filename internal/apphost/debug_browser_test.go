package apphost

import (
	"strings"
	"testing"
)

func TestDebugBrowserArgsRequireExplicitValidWindowsConfiguration(t *testing.T) {
	base := DebugBoot{Enabled: true, CDPEnabled: true, CDPPort: 9223}
	got := debugWindowsOptionsForPlatform(base, "windows")
	joined := strings.Join(got.AdditionalBrowserArgs, " ")
	if !strings.Contains(joined, "--remote-debugging-port=9223") || !strings.Contains(joined, "--remote-debugging-address=127.0.0.1") {
		t.Fatalf("browser args = %q", joined)
	}
	for _, platform := range []string{"darwin", "linux"} {
		if len(debugWindowsOptionsForPlatform(base, platform).AdditionalBrowserArgs) != 0 {
			t.Fatalf("CDP configured for %s", platform)
		}
	}
	for _, bad := range []DebugBoot{{Enabled: false, CDPEnabled: true, CDPPort: 9223}, {Enabled: true, CDPEnabled: false, CDPPort: 9223}, {Enabled: true, CDPEnabled: true, CDPPort: 0}, {Enabled: true, CDPEnabled: true, CDPPort: 65536}} {
		if len(debugWindowsOptionsForPlatform(bad, "windows").AdditionalBrowserArgs) != 0 {
			t.Fatalf("CDP configured for invalid boot: %+v", bad)
		}
	}
}
