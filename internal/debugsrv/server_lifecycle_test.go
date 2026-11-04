package debugsrv

import (
	"context"
	"testing"
	"time"
)

// Start 返回后立即 Stop 是宿主测试和应用快速切换调试设置时的合法时序。
// Serve goroutine 必须持有本次启动的 server 实例，不能在 Stop 清空字段后再取 s.srv。
func TestServerImmediateStopDoesNotRaceServe(t *testing.T) {
	for range 100 {
		s := New(Options{
			Addr:    "127.0.0.1:0",
			Handler: &fakeHandler{},
		})
		if _, err := s.Start(); err != nil {
			t.Fatalf("启动调试服务失败: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := s.Stop(ctx)
		cancel()
		if err != nil {
			t.Fatalf("立即停止调试服务失败: %v", err)
		}
	}
}
