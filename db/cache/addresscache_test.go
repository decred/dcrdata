// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package cache

import (
	"testing"
	"time"
)

func TestCacheLock_TryLock(t *testing.T) {
	cl := NewCacheLock()

	addr := "blah"
	busy, wait, done := cl.TryLock(addr)
	if busy {
		t.Fatal("should not be busy")
	}
	if wait != nil {
		t.Fatal("wait should be a nil channel")
	}

	busy2, wait2, _ := cl.TryLock(addr)
	if !busy2 {
		t.Fatal("should be busy")
	}
	if wait2 == nil {
		t.Fatal("wait2 should not be nil")
	}

	go func() {
		time.Sleep(2 * time.Second)
		done()
	}()

	t0 := time.Now()
	t.Log("waiting")
	<-wait2
	t.Log("waited for", time.Since(t0))
}
