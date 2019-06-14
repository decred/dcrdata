module github.com/decred/dcrdata/stakedb/v2

go 1.11

require (
	github.com/asdine/storm v2.2.0+incompatible
	github.com/boltdb/bolt v1.3.1 // indirect
	github.com/decred/dcrd/blockchain/stake v1.1.0
	github.com/decred/dcrd/chaincfg v1.5.1
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/database v1.0.3
	github.com/decred/dcrd/dcrutil v1.3.0
	github.com/decred/dcrd/rpcclient/v2 v2.0.0
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrdata/api/types/v3 v3.0.0
	github.com/decred/dcrdata/rpcutils v1.2.0
	github.com/decred/dcrdata/txhelpers/v2 v2.0.0
	github.com/decred/slog v1.0.0
	github.com/dgraph-io/badger v1.5.5-0.20190214192501-3196cc1d7a5f
	github.com/dustin/go-humanize v1.0.0 // indirect
)
