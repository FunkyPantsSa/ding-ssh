package apphost

import (
	"strings"
	"testing"
)

func TestDebugDiscoveryAlwaysUsesDialableLoopbackURL(t *testing.T) {
	for _, bindLAN := range []bool{false, true} {
		c := &debugController{boot: DebugBoot{BindLAN: bindLAN}, port: 8765}
		info := c.info()
		if info["url"] != "http://127.0.0.1:8765" {
			t.Errorf("BindLAN=%v discovery URL is not a client address: %v", bindLAN, info["url"])
		}
		if !strings.Contains(info["streamHint"].(string), "http://127.0.0.1:8765/") {
			t.Errorf("invalid stream URL: %v", info["streamHint"])
		}
	}
}
