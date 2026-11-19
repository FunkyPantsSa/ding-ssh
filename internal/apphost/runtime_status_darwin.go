//go:build darwin && cgo

package apphost

/*
#include <mach/mach.h>

static uint64_t dingResidentBytes(void) {
	mach_task_basic_info_data_t info;
	mach_msg_type_number_t count = MACH_TASK_BASIC_INFO_COUNT;
	if (task_info(mach_task_self(), MACH_TASK_BASIC_INFO, (task_info_t)&info, &count) != KERN_SUCCESS) {
		return 0;
	}
	return info.resident_size;
}
*/
import "C"

import (
	"time"

	"ding-ssh/internal/logx"
)

// darwinRSSLog 进程常驻内存读取失败是降级事件（RSS 记为 0），限流避免轮询刷屏。
var darwinRSSLog = logx.NewSampler(0, 5*time.Minute)

func init() {
	processResidentBytes = func() int64 {
		rss := int64(C.dingResidentBytes())
		if rss <= 0 {
			logx.WarnSampled(darwinRSSLog, "[apphost] 读取进程常驻内存失败，RSS 记为 0: platform=darwin")
		}
		return rss
	}
}
