package trylock

import (
	"sync"
	"testing"
)

func TestTryLock(t *testing.T) {
	mu := &Mutex{}
	if !mu.TryLock() {
		t.Fatal("mutex must be unlocked")
	}
	if mu.TryLock() {
		t.Fatal("mutex must be locked")
	}

	mu.Unlock()
	if !mu.TryLock() {
		t.Fatal("mutex must be unlocked")
	}
	if mu.TryLock() {
		t.Fatal("mutex must be locked")
	}

	mu.Unlock()
	mu.Lock()
	if mu.TryLock() {
		t.Fatal("mutex must be locked")
	}
	if mu.TryLock() {
		t.Fatal("mutex must be locked")
	}
	mu.Unlock()
}

func TestTryLockRace(t *testing.T) {
	var wg sync.WaitGroup
	mu := new(Mutex)
	var x int
	for i := 0; i < 1024; i++ {
		if i%2 == 0 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if mu.TryLock() {
					x++
					mu.Unlock()
				}
			}()
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			x++
			mu.Unlock()
		}()
	}
	wg.Wait()
}
