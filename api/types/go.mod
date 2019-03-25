module github.com/decred/dcrdata/api/types

go 1.11

require (
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrjson/v2 v2.0.0
	github.com/decred/dcrdata/v4 v4.0.0-rc4
)

replace github.com/decred/dcrdata/v4 => ../..
