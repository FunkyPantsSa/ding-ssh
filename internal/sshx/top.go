package sshx

import (
	"ding-ssh/internal/models"
	"regexp"
	"strconv"
	"strings"
)

var topCPUValue = regexp.MustCompile(`([0-9.]+)\s*(us|sy|ni|id|wa|hi|si|st)\b`)
var topMemoryValue = regexp.MustCompile(`([0-9.]+)\s+(total|free|used|buff/cache|avail Mem)`)

// parseTop selects the final frame: the first CPU frame may describe time since boot.
func parseTop(raw string, snap *models.SysInfoSnapshot) {
	if i := strings.LastIndex(raw, "top - "); i >= 0 {
		raw = raw[i:]
	}
	var header map[string]int
	cpuOK, memOK := false, false
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "top -") {
			if i := strings.Index(line, "load average:"); i >= 0 {
				for _, v := range strings.Split(line[i+13:], ",") {
					if n, e := strconv.ParseFloat(strings.TrimSpace(v), 64); e == nil {
						snap.Load = append(snap.Load, n)
					}
				}
			}
		}
		if strings.HasPrefix(line, "%Cpu(s)") || strings.HasPrefix(line, "Cpu(s)") {
			snap.CPUDetail = map[string]float64{}
			for _, m := range topCPUValue.FindAllStringSubmatch(line, -1) {
				n, _ := strconv.ParseFloat(m[1], 64)
				snap.CPUDetail[m[2]] = n
			}
			if idle, ok := snap.CPUDetail["id"]; ok {
				snap.CPUUsage = 100 - idle
				cpuOK = true
			}
		}
		if strings.Contains(line, " Mem :") || strings.Contains(line, " Swap:") || strings.Contains(line, " Swap :") {
			factor := 1.0
			switch {
			case strings.HasPrefix(line, "KiB"):
				factor = 1.0 / 1024
			case strings.HasPrefix(line, "GiB"):
				factor = 1024
			case strings.HasPrefix(line, "TiB"):
				factor = 1024 * 1024
			}
			values := map[string]float64{}
			for _, m := range topMemoryValue.FindAllStringSubmatch(line, -1) {
				n, _ := strconv.ParseFloat(m[1], 64)
				values[m[2]] = n * factor
			}
			if strings.Contains(line, " Mem :") {
				snap.MemTotalMB = uint64(values["total"])
				snap.MemUsedMB = uint64(values["used"])
				memOK = values["total"] > 0
			} else {
				snap.SwapTotalMB = values["total"]
				snap.SwapUsedMB = values["used"]
				snap.MemAvailableMB = values["avail Mem"]
			}
		}
		f := strings.Fields(line)
		if len(f) > 0 && f[0] == "PID" {
			header = map[string]int{}
			for i, k := range f {
				header[k] = i
			}
			continue
		}
		if header == nil || len(f) == 0 {
			continue
		}
		get := func(k string) string {
			i, ok := header[k]
			if !ok || i >= len(f) {
				return ""
			}
			return f[i]
		}
		pid, e := strconv.Atoi(get("PID"))
		if e != nil {
			continue
		}
		cmdIndex, ok := header["COMMAND"]
		if !ok || cmdIndex >= len(f) {
			continue
		}
		cpu, _ := strconv.ParseFloat(get("%CPU"), 64)
		mem, _ := strconv.ParseFloat(get("%MEM"), 64)
		snap.Processes = append(snap.Processes, models.TopProcess{PID: pid, User: get("USER"), CPU: cpu, Memory: mem, Resident: get("RES"), State: get("S"), Time: get("TIME+"), Command: strings.Join(f[cmdIndex:], " ")})
	}
	snap.TopAvailable = cpuOK && memOK
}
