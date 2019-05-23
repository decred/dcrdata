module github.com/decred/dcrdata/mempool

go 1.11

require (
	github.com/decred/dcrd/blockchain v1.1.1
	github.com/decred/dcrd/blockchain/stake v1.1.0
	github.com/decred/dcrd/chaincfg v1.4.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrjson/v2 v2.0.0
	github.com/decred/dcrd/dcrutil v1.2.1-0.20190118223730-3a5281156b73
	github.com/decred/dcrd/rpcclient/v2 v2.0.0
	github.com/decred/dcrdata/api/types v1.0.7-0.20190416204615-70a58657e02f
	github.com/decred/dcrdata/db/dbtypes v1.0.2-0.20190416204615-70a58657e02f
	github.com/decred/dcrdata/explorer/types v1.0.1-0.20190416204615-70a58657e02f
	github.com/decred/dcrdata/pubsub/types v1.0.1-0.20190416204615-70a58657e02f
	github.com/decred/dcrdata/rpcutils v1.0.2-0.20190416204615-70a58657e02f
	github.com/decred/dcrdata/txhelpers v1.0.2-0.20190416161040-1dc819eb191d
	github.com/decred/slog v1.0.0
	github.com/dustin/go-humanize v1.0.0
)
