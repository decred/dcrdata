// Copyright (c) 2016, 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/decred/dcrd/rpcclient/v8"
	"github.com/decred/slog"
	"github.com/jrick/logrotate/rotator"

	"github.com/decred/dcrdata/cmd/dcrdata/internal/api"
	"github.com/decred/dcrdata/cmd/dcrdata/internal/api/insight"
	"github.com/decred/dcrdata/cmd/dcrdata/internal/explorer"
	"github.com/decred/dcrdata/cmd/dcrdata/internal/middleware"
	notify "github.com/decred/dcrdata/cmd/dcrdata/internal/notification"

	"github.com/decred/dcrdata/db/dcrpg/v8"
	"github.com/decred/dcrdata/exchanges/v3"
	"github.com/decred/dcrdata/gov/v6/agendas"
	"github.com/decred/dcrdata/gov/v6/politeia"

	"github.com/decred/dcrdata/v8/blockdata"
	"github.com/decred/dcrdata/v8/mempool"
	"github.com/decred/dcrdata/v8/pubsub"
	"github.com/decred/dcrdata/v8/rpcutils"
	"github.com/decred/dcrdata/v8/stakedb"
)

// logWriter implements an io.Writer that outputs to both standard output and
// the write-end pipe of an initialized log rotator.
type logWriter struct{}

// Write writes the data in p to standard out and the log rotator.
func (logWriter) Write(p []byte) (n int, err error) {
	os.Stdout.Write(p)
	return logRotator.Write(p)
}

// Loggers per subsystem.  A single backend logger is created and all subsytem
// loggers created from it will write to the backend.  When adding new
// subsystems, add the subsystem logger variable here and to the
// subsystemLoggers map.
//
// Loggers can not be used before the log rotator has been initialized with a
// log file.  This must be performed early during application startup by calling
// initLogRotator.
var (
	// backendLog is the logging backend used to create all subsystem loggers.
	// The backend must not be used before the log rotator has been initialized,
	// or data races and/or nil pointer dereferences will occur.
	backendLog = slog.NewBackend(logWriter{})

	// logRotator is one of the logging outputs.  It should be closed on
	// application shutdown.
	logRotator *rotator.Rotator

	notifyLog     = backendLog.Logger("NTFN")
	postgresqlLog = backendLog.Logger("PSQL")
	stakedbLog    = backendLog.Logger("SKDB")
	BlockdataLog  = backendLog.Logger("BLKD")
	clientLog     = backendLog.Logger("RPCC")
	mempoolLog    = backendLog.Logger("MEMP")
	expLog        = backendLog.Logger("EXPR")
	apiLog        = backendLog.Logger("JAPI")
	log           = backendLog.Logger("DATD")
	iapiLog       = backendLog.Logger("IAPI")
	pubsubLog     = backendLog.Logger("PUBS")
	xcBotLog      = backendLog.Logger("XBOT")
	agendasLog    = backendLog.Logger("AGDB")
	proposalsLog  = backendLog.Logger("PRDB")
)

// Initialize package-global logger variables.
func init() {
	dcrpg.UseLogger(postgresqlLog)
	stakedb.UseLogger(stakedbLog)
	blockdata.UseLogger(BlockdataLog)
	rpcclient.UseLogger(clientLog)
	rpcutils.UseLogger(clientLog)
	mempool.UseLogger(mempoolLog)
	explorer.UseLogger(expLog)
	api.UseLogger(apiLog)
	insight.UseLogger(iapiLog)
	middleware.UseLogger(apiLog)
	notify.UseLogger(notifyLog)
	pubsub.UseLogger(pubsubLog)
	exchanges.UseLogger(xcBotLog)
	agendas.UseLogger(agendasLog)
	politeia.UseLogger(proposalsLog)
}

// subsystemLoggers maps each subsystem identifier to its associated logger.
var subsystemLoggers = map[string]slog.Logger{
	"NTFN": notifyLog,
	"PSQL": postgresqlLog,
	"SKDB": stakedbLog,
	"BLKD": BlockdataLog,
	"RPCC": clientLog,
	"MEMP": mempoolLog,
	"EXPR": expLog,
	"JAPI": apiLog,
	"IAPI": iapiLog,
	"DATD": log,
	"PUBS": pubsubLog,
	"XBOT": xcBotLog,
	"AGDB": agendasLog,
	"PRDB": proposalsLog,
}

// initLogRotator initializes the logging rotater to write logs to logFile and
// create roll files in the same directory.  It must be called before the
// package-global log rotater variables are used.
func initLogRotator(logFile string, maxRolls int) {
	logDir, _ := filepath.Split(logFile)
	err := os.MkdirAll(logDir, 0700)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create log directory: %v\n", err)
		os.Exit(1)
	}
	r, err := rotator.New(logFile, 32*1024, false, maxRolls)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create file rotator: %v\n", err)
		os.Exit(1)
	}

	logRotator = r
}

// setLogLevel sets the logging level for provided subsystem.  Invalid
// subsystems are ignored.  Uninitialized subsystems are dynamically created as
// needed.
func setLogLevel(subsystemID string, logLevel string) {
	// Ignore invalid subsystems.
	logger, ok := subsystemLoggers[subsystemID]
	if !ok {
		return
	}

	// Defaults to info if the log level is invalid.
	level, _ := slog.LevelFromString(logLevel)
	logger.SetLevel(level)
}

// setLogLevels sets the log level for all subsystem loggers to the passed
// level.  It also dynamically creates the subsystem loggers as needed, so it
// can be used to initialize the logging system.
func setLogLevels(logLevel string) {
	// Configure all sub-systems with the new logging level.  Dynamically
	// create loggers as needed.
	for subsystemID := range subsystemLoggers {
		setLogLevel(subsystemID, logLevel)
	}
}
