package apphost

import (
	"os"
	"strconv"
	"strings"
	"time"

	"ding-ssh/internal/logx"
)

// linuxStatmLog 读取 /proc/self/statm 失败会退化为 RSS=0，属于轮询路径上的降级事件，限流输出。
var linuxStatmLog = logx.NewSampler(0, 5*time.Minute)

func init() {
	processResidentBytes = func() int64 {
		data, err := os.ReadFile("/proc/self/statm")
		if err != nil {
			logx.WarnSampled(linuxStatmLog, "[apphost] 读取进程内存状态失败，进程 RSS 记为 0: path=/proc/self/statm err=%v", err)
			return 0
		}
		fields := strings.Fields(string(data))
		if len(fields) < 2 {
			logx.WarnSampled(linuxStatmLog, "[apphost] 进程内存状态格式异常，进程 RSS 记为 0: path=/proc/self/statm fields=%d", len(fields))
			return 0
		}
		pages, _ := strconv.ParseInt(fields[1], 10, 64)
		return pages * int64(os.Getpagesize())
	}
}
