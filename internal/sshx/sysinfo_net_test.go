package sshx

import (
	"strings"
	"testing"
)

func TestRankNetIfacesPrefersEnsOverVethNoise(t *testing.T) {
	byName := map[string]netCounter{}
	// 模拟 K8s 节点：大量 veth 排在前面，物理口 ens18 靠后
	for i := 0; i < 40; i++ {
		name := "veth" + strings.Repeat("a", i%5) + string(rune('0'+i%10))
		byName[name] = netCounter{name: name, rx: 1, tx: 1}
	}
	byName["kube-ipvs0"] = netCounter{name: "kube-ipvs0", rx: 100, tx: 100}
	byName["docker0"] = netCounter{name: "docker0", rx: 10, tx: 10}
	byName["ens18"] = netCounter{name: "ens18", rx: 999, tx: 888}
	byName["cali123abc"] = netCounter{name: "cali123abc", rx: 2, tx: 2}

	ips := map[string]string{
		"ens18":      "172.20.50.141",
		"kube-ipvs0": "10.68.105.1",
		"docker0":    "172.17.0.1",
	}

	ranked := rankNetIfaces(byName, ips)
	if len(ranked) == 0 {
		t.Fatal("expected ranked interfaces")
	}
	if ranked[0].name != "ens18" {
		t.Fatalf("expected ens18 first, got %s (all=%v)", ranked[0].name, namesOf(ranked))
	}
	found := false
	for _, n := range ranked {
		if n.name == "ens18" {
			found = true
		}
		if strings.HasPrefix(n.name, "veth") {
			t.Fatalf("veth without IP should be filtered, got %s", n.name)
		}
	}
	if !found {
		t.Fatalf("ens18 missing from ranked list: %v", namesOf(ranked))
	}
}

func TestParseNetRawNoHard16Cap(t *testing.T) {
	var b strings.Builder
	b.WriteString("Inter-|   Receive\n")
	b.WriteString(" face |bytes\n")
	for i := 0; i < 30; i++ {
		b.WriteString("  veth")
		b.WriteByte(byte('a' + i%26))
		b.WriteString(": 1 0 0 0 0 0 0 0 1 0 0 0 0 0 0 0\n")
	}
	b.WriteString("  ens18: 100 0 0 0 0 0 0 0 200 0 0 0 0 0 0 0\n")
	got := parseNetRaw(b.String())
	if len(got) < 31 {
		t.Fatalf("expected all interfaces parsed, got %d", len(got))
	}
	last := got[len(got)-1]
	if last.name != "ens18" {
		t.Fatalf("expected ens18 present, last=%s count=%d", last.name, len(got))
	}
}

func namesOf(list []netCounter) []string {
	out := make([]string, len(list))
	for i, n := range list {
		out[i] = n.name
	}
	return out
}
