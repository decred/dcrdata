// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package notification

import (
	"errors"
)

// ContextualError models an error and its root error and is returned by
// registerNodeNtfnHandlers
type ContextualError struct {
	RootErr error
	Err     error
}

func (e *ContextualError) Error() string {
	return e.Err.Error()
}

// Cause returns the root error string
func (e *ContextualError) Cause() string {
	return e.RootErr.Error()
}

func newContextualError(context string, err error) *ContextualError {
	return &ContextualError{
		RootErr: err,
		Err:     errors.New(context),
	}
}
