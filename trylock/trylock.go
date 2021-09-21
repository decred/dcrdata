// Copyright (c) 2021, The Decred developers
// See LICENSE for details.

package trylock

import (
	"math/rand"
	"sync"
	"time"
)

// Mutex is a "try lock" for coordinating multiple accessors, while allowing
// only a single updater. It is not very smart about queueing when there are
// multiple callers of Lock waiting.
type Mutex struct {
	mtx sync.Mutex
	c   chan struct{}
}

// tryLock will attempt to obtain an exclusive lock or a non-nil wait channel.
// If the lock is already held, the returned channel will be non-nil and the
// caller should wait for it to be closed before trying again. There is no
// queueing. Use the Lock method to block until it is acquired.
//
// tryLock returns a bool, busy, indicating if another caller has already
// obtained the lock. When busy is false, the caller has obtained the exclusive
// lock, and Unlock should be called when ready to release the lock. When busy
// is true, the returned channel should be received from to block until the
// updater has released the lock.
func (tl *Mutex) tryLock() (busy bool, wait chan struct{}) {
	tl.mtx.Lock()
	defer tl.mtx.Unlock()
	if tl.c == nil {
		tl.c = make(chan struct{})
		return false, nil
	}
	return true, tl.c
}

// TryLock attempts to acquire the lock. If it returns true, the caller has
// obtained it exclusively, and should call Unlock when done with it. If it
// returns false, the lock was already held.
func (tl *Mutex) TryLock() bool {
	busy, _ := tl.tryLock()
	return !busy // if true, caller must Unlock
}

// Unlock releases the lock. Only the caller of Lock, or TryLock when true was
// returned, should call Unlock.
func (tl *Mutex) Unlock() {
	tl.mtx.Lock()
	defer tl.mtx.Unlock()
	close(tl.c)
	tl.c = nil
}

const maxDelay int64 = 5000 // microseconds

// Lock acquires the lock. If it is already held, this blocks until it can be
// obtained.
func (tl *Mutex) Lock() {
	var i int64
	for { // there is no queue, just a race
		busy, wait := tl.tryLock()
		if !busy {
			return // caller must do Unlock to close and nil out c
		}
		// otherwise wait and try again
		<-wait
		if i > 0 && i < maxDelay { // one quick retry, then randomish delay up to maxDelay retires, then priority
			time.Sleep(time.Duration(rand.Int63n(maxDelay-i)) * time.Microsecond)
		}
		i++
	}
}
