package metrics

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestSetLogWarnFn_Concurrent(t *testing.T) {
	original := logWarnFn.Load()
	t.Cleanup(func() { logWarnFn.Store(original) })

	var got atomic.Int32
	SetLogWarnFn(func(string) { got.Add(1) })

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logWarnf("concurrent warn %d", i)
			logWarnln("concurrent line")
			logErrorf("concurrent error %d", i)
		}()
	}
	// 并发注入（模拟多次 logger.Init）
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			SetLogWarnFn(func(string) { got.Add(1) })
		}()
	}
	wg.Wait()

	if got.Load() == 0 {
		t.Error("日志出口未被调用")
	}
}
