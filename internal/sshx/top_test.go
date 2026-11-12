package sshx

import (
	"ding-ssh/internal/models"
	"testing"
)

func TestTopFinalFrame(t *testing.T) {
	raw := `top - 09:00:00 up 2 days, 1 user, load average: 9.00, 8.00, 7.00
%Cpu(s): 99.0 us, 0.0 sy, 0.0 ni, 1.0 id, 0.0 wa, 0.0 hi, 0.0 si, 0.0 st
MiB Mem : 1000.0 total, 100.0 free, 900.0 used, 0.0 buff/cache
 PID USER PR NI VIRT RES SHR S %CPU %MEM TIME+ COMMAND
 1 root 20 0 1 1 1 S 99 90 0:01 old

top - 09:00:01 up 2 days, 1 user, load average: 1.20, 2.30, 3.40
%Cpu(s): 20.0 us, 5.0 sy, 1.0 ni, 60.0 id, 10.0 wa, 1.0 hi, 1.0 si, 2.0 st
MiB Mem : 2048.0 total, 200.0 free, 1024.0 used, 824.0 buff/cache
MiB Swap: 512.0 total, 500.0 free, 12.0 used. 900.0 avail Mem
 PID USER PR NI VIRT RES SHR S %CPU %MEM TIME+ COMMAND
 42 app 20 0 100m 20m 10m R 215.5 1.0 12:34.56 worker --name hello world
`
	var snap models.SysInfoSnapshot
	parseTop(raw, &snap)
	if !snap.TopAvailable || snap.CPUUsage != 40 || snap.CPUDetail["wa"] != 10 {
		t.Fatalf("CPU: %+v", snap)
	}
	if snap.MemTotalMB != 2048 || snap.MemUsedMB != 1024 || snap.SwapUsedMB != 12 || snap.MemAvailableMB != 900 {
		t.Fatalf("memory: %+v", snap)
	}
	if len(snap.Load) != 3 || snap.Load[0] != 1.2 {
		t.Fatalf("load: %v", snap.Load)
	}
	if len(snap.Processes) != 1 || snap.Processes[0].CPU != 215.5 || snap.Processes[0].Command != "worker --name hello world" {
		t.Fatalf("processes: %+v", snap.Processes)
	}
}

func TestTopMissingAndUnits(t *testing.T) {
	var snap models.SysInfoSnapshot
	parseTop("top: not found", &snap)
	if snap.TopAvailable {
		t.Fatal("missing top must not be valid")
	}
	parseTop("%Cpu(s): 100.0 id\nKiB Mem : 2097152 total, 1048576 used\nKiB Swap: 1048576 total, 0 used. 524288 avail Mem", &snap)
	if !snap.TopAvailable || snap.MemTotalMB != 2048 || snap.MemAvailableMB != 512 {
		t.Fatalf("KiB: %+v", snap)
	}
}
