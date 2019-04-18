module github.com/decred/dcrdata/db/cache

go 1.11

replace github.com/decred/dcrdata/db/dcrpg => ../dcrpg

require (
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrdata/api/types/v2 v2.0.1
	github.com/decred/dcrdata/db/dbtypes v1.1.0
	github.com/decred/slog v1.0.0
)
