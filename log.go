// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"

	"github.com/btcsuite/btclog"
	"github.com/btcsuite/seelog"
)

// Loggers per subsytem.  Note that backendLog is a seelog logger that all of
// the subsystem loggers route their messages to.  When adding new subsystems,
// add a reference here, to the subsystemLoggers map, and the useLogger
// function.
var (
	backendLog = seelog.Disabled
	log        = btclog.Disabled
	daemonLog  = btclog.Disabled
	clientLog  = btclog.Disabled
	mempoolLog = btclog.Disabled
	apiLog     = btclog.Disabled
)

// subsystemLoggers maps each subsystem identifier to its associated logger.
var subsystemLoggers = map[string]btclog.Logger{
	"DDAT": log,
	"DCRD": daemonLog,
	"RPCC": clientLog,
	"MEMP": mempoolLog,
	"JAPI": apiLog,
}

// logClosure is used to provide a closure over expensive logging operations
// so don't have to be performed when the logging level doesn't warrant it.
type logClosure func() string

// String invokes the underlying function and returns the result.
func (c logClosure) String() string {
	return c()
}

// newLogClosure returns a new closure over a function that returns a string
// which itself provides a Stringer interface so that it can be used with the
// logging system.
func newLogClosure(c func() string) logClosure {
	return logClosure(c)
}

// useLogger updates the logger references for subsystemID to logger.  Invalid
// subsystems are ignored.
func useLogger(subsystemID string, logger btclog.Logger) {
	if _, ok := subsystemLoggers[subsystemID]; !ok {
		return
	}
	subsystemLoggers[subsystemID] = logger

	switch subsystemID {
	case "DDAT":
		log = logger
	case "DCRD":
		daemonLog = logger
	case "RPCC":
		clientLog = logger
	case "MEMP":
		mempoolLog = logger
	case "JAPI":
		apiLog = logger
	}
}

// initSeelogLogger initializes a new seelog logger that is used as the backend
// for all logging subsytems.
func initSeelogLogger(logFile string) {
	config := `
        <seelog type="adaptive" mininterval="2000000" maxinterval="100000000"
                critmsgcount="500" minlevel="trace">
                <outputs formatid="all">
                        <console />
                        <rollingfile type="size" filename="%s" maxsize="10485760" maxrolls="3" />
                </outputs>
                <formats>
                        <format id="all" format="%%Date(15:04:05.00 2006-01-02) [%%LEV] %%Msg%%n" />
                </formats>
        </seelog>`
	config = fmt.Sprintf(config, logFile)

	logger, err := seelog.LoggerFromConfigAsString(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create logger: %v", err)
		os.Exit(1)
	}

	backendLog = logger
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

	// Default to info if the log level is invalid.
	level, ok := btclog.LogLevelFromString(logLevel)
	if !ok {
		level = btclog.InfoLvl
	}

	// Create new logger for the subsystem if needed.
	if logger == btclog.Disabled {
		logger = btclog.NewSubsystemLogger(backendLog, subsystemID+": ")
		useLogger(subsystemID, logger)
	}
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

// pickNoun returns the singular or plural form of a noun depending
// on the count n.
func pickNoun(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

// fatalf logs a string, then cleanly exits.
func fatalf(str string) {
	log.Errorf("Unable to create profiler: %v", str)
	backendLog.Flush()
	os.Exit(1)
}
