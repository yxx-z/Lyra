// internal/scanner/watcher_test.go
package scanner

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestDebounce_MultipleEventsCollapse(t *testing.T) {
	var callCount atomic.Int32
	fn := func() { callCount.Add(1) }

	d := newDebouncer(100 * time.Millisecond)
	// 触发 5 次，应该只执行一次
	for i := 0; i < 5; i++ {
		d.trigger("key", fn)
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)

	if callCount.Load() != 1 {
		t.Errorf("callCount = %d, want 1", callCount.Load())
	}
}

func TestDebounce_DifferentKeysIndependent(t *testing.T) {
	var countA, countB atomic.Int32
	d := newDebouncer(50 * time.Millisecond)
	d.trigger("a", func() { countA.Add(1) })
	d.trigger("b", func() { countB.Add(1) })
	time.Sleep(150 * time.Millisecond)

	if countA.Load() != 1 || countB.Load() != 1 {
		t.Errorf("a=%d b=%d, want 1 1", countA.Load(), countB.Load())
	}
}
