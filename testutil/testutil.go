// Copyright (c) 2018, The Decred developers
// Use of this source code is governed by an ISC license
// that can be found in the LICENSE file.

// package testutil provides convenience functions and types for testing.
package testutil

import (
	"fmt"
	"testing"
)

var t *testing.T

// CurrentTestSetup returns the active test setup.
func CurrentTestSetup() *testing.T {
	return t
}

// BindCurrentTestSetup assigns the active testing framework.
func BindCurrentTestSetup(set *testing.T) {
	t = set
}

// UseSpewToPrettyPrintArrays indicates that the go-spew package should be used
// to pretty print arrays.
var UseSpewToPrettyPrintArrays = true

// PanicOnTestSetupFailure indicates if test setup failure should cause a panic.
var PanicOnTestSetupFailure = true

// PanicOnTestFailure indicates if a stake trace should be printed on test
// failure.
var PanicOnTestFailure = false

// ReportTestIsNotAbleToTest called on failure in a test setup
// Indicates that test needs to be fixed
func ReportTestIsNotAbleToTest(report string, args ...interface{}) {
	errorMessage := "test setup failure: " + fmt.Sprintf(report, args...)
	if PanicOnTestSetupFailure || t == nil {
		panic(errorMessage)
	} else {
		t.Fatal(errorMessage)
	}
}

// ReportTestFailed called by test-code to indicate the code failed the test.
// ReportTestFailed calls testing.T.Fatalf() for tests executed with go-test.
// Otherwise it brings attention on a bug with the panic(). Ideally this should
// happen when the test-check was performed by an assert() call in a running
// program and revealed unacceptable behaviour during debugging
// or in the test-net.
func ReportTestFailed(msg string, args ...interface{}) {
	errorMessage := fmt.Sprintf(msg, args...)
	if t == nil || PanicOnTestFailure {
		panic(errorMessage)
	} else {
		t.Fatal(errorMessage)
	}
}

// TestName returns the name of the active test setup.
func TestName() string {
	return t.Name()
}
