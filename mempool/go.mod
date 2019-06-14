module github.com/decred/dcrdata/mempool

go 1.11

require (
	github.com/decred/dcrd/blockchain v1.1.1
	github.com/decred/dcrd/blockchain/stake v1.1.0
	github.com/decred/dcrd/chaincfg v1.5.1
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrjson/v2 v2.0.0
	github.com/decred/dcrd/dcrutil v1.3.0
	github.com/decred/dcrd/rpcclient/v2 v2.0.0
	github.com/decred/dcrdata/api/types v1.0.7-0.20190416204615-70a58657e02f
	github.com/decred/dcrdata/db/dbtypes v1.0.2-0.20190416204615-70a58657e02f
	github.com/decred/dcrdata/explorer/types v1.1.0
	github.com/decred/dcrdata/pubsub/types v1.0.1-0.20190416204615-70a58657e02f
	github.com/decred/dcrdata/rpcutils v1.0.2-0.20190416204615-70a58657e02f
	github.com/decred/dcrdata/txhelpers/v2 v2.0.0
	github.com/decred/slog v1.0.0
	github.com/dustin/go-humanize v1.0.0
)
