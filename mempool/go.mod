module github.com/decred/dcrdata/mempool/v2

go 1.11

replace github.com/decred/dcrdata/db/dcrpg => ../db/dcrpg

require (
	github.com/decred/dcrd/blockchain v1.1.1
	github.com/decred/dcrd/blockchain/stake v1.1.0
	github.com/decred/dcrd/chaincfg v1.4.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrjson/v2 v2.0.0
	github.com/decred/dcrd/dcrutil v1.2.1-0.20190118223730-3a5281156b73
	github.com/decred/dcrd/rpcclient/v2 v2.0.0
	github.com/decred/dcrdata/api/types/v2 v2.0.1
	github.com/decred/dcrdata/db/dbtypes v1.1.0
	github.com/decred/dcrdata/explorer/types v1.0.1
	github.com/decred/dcrdata/pubsub/types v1.0.1
	github.com/decred/dcrdata/rpcutils v1.1.0
	github.com/decred/dcrdata/txhelpers v1.1.0
	github.com/decred/slog v1.0.0
	github.com/dustin/go-humanize v1.0.0
)
