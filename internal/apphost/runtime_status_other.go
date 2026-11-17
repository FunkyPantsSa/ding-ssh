//go:build !darwin && !linux && !windows

package apphost

func addProcessUsage(status map[string]int64) {}
