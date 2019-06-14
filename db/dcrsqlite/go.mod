module github.com/decred/dcrdata/db/dcrsqlite/v3

go 1.11

require (
	github.com/boltdb/bolt v1.3.1 // indirect
	github.com/decred/dcrd/blockchain/stake v1.1.0
	github.com/decred/dcrd/chaincfg v1.5.1
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrjson/v2 v2.0.0
	github.com/decred/dcrd/dcrutil v1.3.0
	github.com/decred/dcrd/rpcclient/v2 v2.0.0
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrdata/api/types/v3 v3.0.0
	github.com/decred/dcrdata/blockdata/v2 v2.0.0
	github.com/decred/dcrdata/db/cache/v2 v2.0.0
	github.com/decred/dcrdata/db/dbtypes/v2 v2.0.0
	github.com/decred/dcrdata/explorer/types v1.1.0
	github.com/decred/dcrdata/mempool/v3 v3.0.1
	github.com/decred/dcrdata/rpcutils v1.2.0
	github.com/decred/dcrdata/stakedb/v2 v2.0.0
	github.com/decred/dcrdata/testutil/dbconfig v1.0.1
	github.com/decred/dcrdata/txhelpers/v2 v2.0.0
	github.com/decred/slog v1.0.0
	github.com/dustin/go-humanize v1.0.0
	github.com/google/go-cmp v0.2.0
	github.com/mattn/go-sqlite3 v1.10.0
)
