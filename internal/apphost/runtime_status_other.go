//go:build !darwin && !linux && !windows

package apphost

import (
	"time"

	"ding-ssh/internal/logx"
)

// otherPlatformUsageLog 该平台不支持进程资源采集；轮询路径用限流避免刷屏。
var otherPlatformUsageLog = logx.NewSampler(0, 5*time.Minute)

func addProcessUsage(status map[string]int64) {
	logx.DebugSampled(otherPlatformUsageLog, "[apphost] 当前平台不支持进程资源采集，已跳过")
}
