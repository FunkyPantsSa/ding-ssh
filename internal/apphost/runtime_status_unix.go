//go:build darwin || linux

package apphost

import (
	"runtime"

	"golang.org/x/sys/unix"
)

var processResidentBytes = func() int64 { return 0 }

func addProcessUsage(status map[string]int64) {
	if rss := processResidentBytes(); rss > 0 {
		status["processRSSBytes"] = rss
	}
	var usage unix.Rusage
	if unix.Getrusage(unix.RUSAGE_SELF, &usage) != nil {
		return
	}
	status["processCPUms"] = usage.Utime.Nano()/1e6 + usage.Stime.Nano()/1e6
	status["processPeakRSSBytes"] = usage.Maxrss
	if runtime.GOOS == "linux" {
		status["processPeakRSSBytes"] *= 1024
	}
}
