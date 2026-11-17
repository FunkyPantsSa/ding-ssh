package apphost

import (
	"os"
	"strconv"
	"strings"
)

func init() {
	processResidentBytes = func() int64 {
		data, err := os.ReadFile("/proc/self/statm")
		if err != nil {
			return 0
		}
		fields := strings.Fields(string(data))
		if len(fields) < 2 {
			return 0
		}
		pages, _ := strconv.ParseInt(fields[1], 10, 64)
		return pages * int64(os.Getpagesize())
	}
}
