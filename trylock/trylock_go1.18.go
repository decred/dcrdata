// Copyright (c) 2022, The Decred developers
// See LICENSE for details.

//go:build go1.18
// +build go1.18

package trylock

import "sync"

// Mutex is just an alias for sync.Mutex in Go 1.18 onward since TryLock() was
// added to the standard library.
type Mutex = sync.Mutex
