// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"path/filepath"

	"github.com/decred/dcrdata/v4/exchanges"
	"github.com/decred/slog"
	"github.com/jrick/logrotate/rotator"
)

var (
	log        = slog.Disabled
	logRotator *rotator.Rotator
	backendLog = slog.NewBackend(logWriter{})
	xcLogger   = backendLog.Logger("XBOT")
)

// logWriter implements an io.Writer that outputs to both standard output and
// the write-end pipe of an initialized log rotator.
type logWriter struct{}

// Write writes the data in p to standard out and the log rotator.
func (logWriter) Write(p []byte) (n int, err error) {
	os.Stdout.Write(p)
	return logRotator.Write(p)
}

// inititializeLogging initializes the logging rotater to write logs to logFile
// and create roll files in the same directory.  It must be called before the
// package-global log rotater variables are used.
func inititializeLogging(logFile string) {
	log = backendLog.Logger("SRVR")
	exchanges.UseLogger(backendLog.Logger("XBOT"))
	logDir, _ := filepath.Split(logFile)
	err := os.MkdirAll(logDir, 0700)
	if err != nil {
		log.Errorf("failed to create log directory: %v", err)
		os.Exit(1)
	}
	r, err := rotator.New(logFile, 10*1024, false, 3)
	if err != nil {
		log.Errorf("failed to create file rotator: %v\n", err)
		os.Exit(1)
	}
	logRotator = r
}
