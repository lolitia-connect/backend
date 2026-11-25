package syncx

import (
	"testing"
	"testing/synctest"
	"time"
)

func TestTimeoutCondWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cond := NewCond()
		done := make(chan struct{})
		go func() {
			cond.Wait()
			close(done)
		}()
		synctest.Wait()
		cond.Signal()
		<-done
	})
}

func TestTimeoutCondWaitTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const timeout = 500 * time.Millisecond
		start := time.Now()
		remaining, signaled := NewCond().WaitWithTimeout(timeout)
		if elapsed := time.Since(start); signaled || remaining != 0 || elapsed != timeout {
			t.Fatalf("wait without signal returned remaining=%v, signaled=%v after %v", remaining, signaled, elapsed)
		}
	})
}

func TestTimeoutCondWaitTimeoutRemain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const timeout = 2 * time.Second
		const elapsed = 200 * time.Millisecond
		cond := NewCond()
		go func() {
			time.Sleep(elapsed)
			cond.Signal()
		}()
		remaining, signaled := cond.WaitWithTimeout(timeout)
		if !signaled || remaining != timeout-elapsed {
			t.Fatalf("wait returned remaining=%v, signaled=%v, want %v and true", remaining, signaled, timeout-elapsed)
		}
	})
}

func TestSignalNoWait(t *testing.T) {
	cond := NewCond()
	cond.Signal()
}
