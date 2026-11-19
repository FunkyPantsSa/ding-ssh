package sshx

import (
	"context"
	"sync"
	"testing"
	"time"
)

// 验证「实时刷新系统信息」开关的真实行为：
//   - 关闭时只采集一次初始快照，之后不再轮询；
//   - 打开后按当前间隔周期采集，不需要重启采集器。
func TestSysInfoLoopPollsOnlyWhenRealtimeEnabled(t *testing.T) {
	oldActive, oldIdle := sysInfoIntervalActive, sysInfoIntervalIdle
	sysInfoIntervalActive, sysInfoIntervalIdle = 10*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { sysInfoIntervalActive, sysInfoIntervalIdle = oldActive, oldIdle })

	c := newSysInfoCollector("test", nil, nil, nil)
	var mu sync.Mutex
	count := 0
	realtime := false
	c.collectFn = func(context.Context) {
		mu.Lock()
		count++
		mu.Unlock()
	}
	c.realtime = func() bool {
		mu.Lock()
		defer mu.Unlock()
		return realtime
	}

	c.Start()
	time.Sleep(60 * time.Millisecond)
	mu.Lock()
	initial := count
	mu.Unlock()
	if initial != 1 {
		t.Fatalf("实时刷新关闭时应只采集一次，实际 %d 次", initial)
	}

	mu.Lock()
	realtime = true
	mu.Unlock()
	time.Sleep(60 * time.Millisecond)
	mu.Lock()
	after := count
	mu.Unlock()
	if after < 2 {
		t.Fatalf("开启实时刷新后应继续采集，实际仍为 %d 次", after)
	}
	c.Stop()
}
