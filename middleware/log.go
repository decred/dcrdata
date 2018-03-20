// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package middleware

import "github.com/btcsuite/btclog"

// log is a logger that is initialized with no output filters.  This
// means the package will not perform any logging by default until the caller
// requests it.
var apiLog = btclog.Disabled

// DisableLog disables all library log output.  Logging output is disabled
// by default until UseLogger is called.
func DisableLog() {
	apiLog = btclog.Disabled
}

// UseLogger uses a specified Logger to output package logging info.
func UseLogger(logger btclog.Logger) {
	apiLog = logger
}
