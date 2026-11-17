package apphost

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var getProcessMemoryInfo = windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

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
	}

	var mem processMemoryCounters
	mem.size = uint32(unsafe.Sizeof(mem))
	result, _, _ := getProcessMemoryInfo.Call(uintptr(process), uintptr(unsafe.Pointer(&mem)), uintptr(mem.size))
	if result != 0 {
		status["processRSSBytes"] = int64(mem.workingSet)
		status["processPeakRSSBytes"] = int64(mem.peakWorkingSet)
	}
}
