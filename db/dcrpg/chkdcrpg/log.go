// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"path/filepath"

	"github.com/decred/dcrd/rpcclient/v7"
	"github.com/decred/dcrdata/db/dcrpg/v7"
	"github.com/decred/dcrdata/v7/rpcutils"
	"github.com/decred/dcrdata/v7/stakedb"
	"github.com/decred/slog"
	"github.com/jrick/logrotate/rotator"
)

var (
	logRotator *rotator.Rotator
	backendLog = slog.NewBackend(logWriter{})
	log        = backendLog.Logger("CHKDB")
	dbLogger   = backendLog.Logger("PSQL")
	sdbLogger  = backendLog.Logger("SKDB")
	rpcLogger  = backendLog.Logger("RPCC")
)

// logWriter implements an io.Writer that outputs to both standard output and
// the write-end pipe of an initialized log rotator.
type logWriter struct{}

// Write writes the data in p to standard out and the log rotator.
func (logWriter) Write(p []byte) (n int, err error) {
	os.Stdout.Write(p)
	return logRotator.Write(p)
}

// initializeLogging initializes the logging rotater to write logs to logFile
// and create roll files in the same directory.  It must be called before the
// package-global log rotater variables are used.
func initializeLogging(logFile, logLevel string) {
	dcrpg.UseLogger(dbLogger)
	stakedb.UseLogger(sdbLogger)
	rpcclient.UseLogger(rpcLogger)
	rpcutils.UseLogger(rpcLogger)

	logDir, _ := filepath.Split(logFile)
	err := os.MkdirAll(logDir, 0700)
	if err != nil {
		log.Errorf("failed to create log directory: %v", err)
		os.Exit(1)
	}
	r, err := rotator.New(logFile, 10*1024, false, 16)
	if err != nil {
		log.Errorf("failed to create file rotator: %v\n", err)
		os.Exit(1)
	}
	logRotator = r
	if logLevel != "" {
		level, ok := slog.LevelFromString(logLevel)
		if ok {
			log.SetLevel(level)
			dbLogger.SetLevel(level)
			sdbLogger.SetLevel(level)
			rpcLogger.SetLevel(level)
		} else {
			log.Infof("Unable to assign logging level=%s. Falling back to level=info", logLevel)
		}
	}
}
