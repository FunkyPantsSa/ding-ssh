//go:build darwin || linux

package apphost

import (
	"runtime"
	"time"

	"golang.org/x/sys/unix"

	"ding-ssh/internal/logx"
)

var processResidentBytes = func() int64 { return 0 }

// unixRusageLog 进程资源采集失败属于轮询路径上的降级事件，限流输出。
var unixRusageLog = logx.NewSampler(0, 5*time.Minute)

func addProcessUsage(status map[string]int64) {
	if rss := processResidentBytes(); rss > 0 {
		status["processRSSBytes"] = rss
	}
	var usage unix.Rusage
	if unix.Getrusage(unix.RUSAGE_SELF, &usage) != nil {
		logx.WarnSampled(unixRusageLog, "[apphost] 读取进程资源用量失败，CPU/峰值内存记为 0: platform=%s", runtime.GOOS)
		return
	}
	status["processCPUms"] = usage.Utime.Nano()/1e6 + usage.Stime.Nano()/1e6
	status["processPeakRSSBytes"] = usage.Maxrss
	if runtime.GOOS == "linux" {
		status["processPeakRSSBytes"] *= 1024
	}
}
