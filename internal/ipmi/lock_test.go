package ipmi

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestLockHostSerialisesPerHost checks that commands to one BMC never
// overlap (the iDRAC9 "insufficient resources for session" failure) while
// commands to different BMCs still run concurrently.
func TestLockHostSerialisesPerHost(t *testing.T) {
	var active, peak int32
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := lockHost("10.0.0.1")
			n := atomic.AddInt32(&active, 1)
			for {
				p := atomic.LoadInt32(&peak)
				if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt32(&active, -1)
			unlock()
		}()
	}
	wg.Wait()
	if peak != 1 {
		t.Errorf("same host: %d commands ran at once, want 1", peak)
	}

	unlockA := lockHost("10.0.0.2")
	done := make(chan struct{})
	go func() {
		unlockB := lockHost("10.0.0.3")
		unlockB()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("lock on one host blocked a different host")
	}
	unlockA()
}
