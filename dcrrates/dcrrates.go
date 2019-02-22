// Copyright (c) 2019, The Decred developers
// See LICENSE for details.
package dcrrates

import "github.com/decred/dcrd/dcrutil"

const (
	DefaultKeyName  = "rpc.key"
	DefaultCertName = "rpc.cert"
)

var (
	DefaultAppDirectory = dcrutil.AppDataDir("dcrrates", false)
)
