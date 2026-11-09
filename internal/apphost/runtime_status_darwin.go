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

func init() {
	processResidentBytes = func() int64 { return int64(C.dingResidentBytes()) }
}
