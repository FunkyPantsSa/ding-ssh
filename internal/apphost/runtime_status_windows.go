package apphost

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"ding-ssh/internal/logx"
)

var getProcessMemoryInfo = windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

// windowsProcUsageLog 进程资源采集失败属于轮询路径上的降级事件，限流输出。
var windowsProcUsageLog = logx.NewSampler(0, 5*time.Minute)

type processMemoryCounters struct {
	size, pageFaultCount                     uint32
	peakWorkingSet, workingSet               uintptr
	quotaPeakPagedPool, quotaPagedPool       uintptr
	quotaPeakNonPagedPool, quotaNonPagedPool uintptr
	pagefileUsage, peakPagefileUsage         uintptr
}

func addProcessUsage(status map[string]int64) {
	process := windows.CurrentProcess()
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(process, &created, &exited, &kernel, &user) == nil {
		ticks := func(value windows.Filetime) uint64 {
			return uint64(value.HighDateTime)<<32 | uint64(value.LowDateTime)
		}
		status["processCPUms"] = int64((ticks(kernel) + ticks(user)) / 10000)
	} else {
		logx.WarnSampled(windowsProcUsageLog, "[apphost] 读取进程 CPU 时间失败，CPU 记为 0")
	}

	var mem processMemoryCounters
	mem.size = uint32(unsafe.Sizeof(mem))
	result, _, _ := getProcessMemoryInfo.Call(uintptr(process), uintptr(unsafe.Pointer(&mem)), uintptr(mem.size))
	if result != 0 {
		status["processRSSBytes"] = int64(mem.workingSet)
		status["processPeakRSSBytes"] = int64(mem.peakWorkingSet)
	} else {
		logx.WarnSampled(windowsProcUsageLog, "[apphost] 读取进程内存信息失败，RSS 记为 0")
	}
}
