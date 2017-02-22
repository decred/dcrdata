// Copyright (c) 2017, Jonathan Chappelow
// See LICENSE for details.

package main

import (
	"errors"
)

type ContextualError struct {
	RootErr error
	Err     error
}

func (e *ContextualError) Error() string {
	return e.Err.Error()
}

func (e *ContextualError) Cause() string {
	return e.RootErr.Error()
}

func newContextualError(context string, err error) *ContextualError {
	return &ContextualError{
		RootErr: err,
		Err:     errors.New(context),
	}
}
