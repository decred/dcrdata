// Copyright (c) 2018+ The Decred developers
// Use of this source code is governed by an ISC license
// that can be found in the LICENSE file.

/*
Package testutil provides convenience functions and types for testing.

Generally tests should be written in a way that helps easily to switch
to any other test framework. For example, this allows reusing test-checks
by calling them from working prod code like assert calls.

testutil stands as a proxy between user-code and a test framework giving
some flexibility on configuring test-setup.

Currently testutil wraps only the GO-Lang/testing framework
*/

package testutil

import (
	"fmt"
	"testing"
)

var t *testing.T

func CurrentTestSetup() *testing.T {
	return t
}

// BindCurrentTestSetup deploys GO-Lang/testing framework
func BindCurrentTestSetup(set *testing.T) {
	t = set
}

// Set this flag "true" to use "github.com/davecgh/go-spew/spew"
// for arrays pretty print
var UseSpewToPrettyPrintArrays = true

// Defines what to do on test setup crash
var PanicOnTestSetupFailure = true

// Set this flag "true" to print a stack stace when a test failed
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

// TestName returns current test name for tests executed with go-test
func TestName() string {
	return t.Name()
}
