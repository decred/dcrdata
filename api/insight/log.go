// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package insight

import "github.com/decred/slog"

// log is a logger that is initialized with no output filters.  This
// means the package will not perform any logging by default until the caller
// requests it.
var apiLog = slog.Disabled

// DisableLog disables all library log output.  Logging output is disabled
// by default until UseLogger is called.
func DisableLog() {
	apiLog = slog.Disabled
}

// UseLogger uses a specified Logger to output package logging info.
func UseLogger(logger slog.Logger) {
	apiLog = logger
}
