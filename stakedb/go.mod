module github.com/decred/dcrdata/stakedb

go 1.11

replace github.com/decred/dcrdata/db/dcrpg => ../db/dcrpg

require (
	github.com/asdine/storm v2.2.0+incompatible
	github.com/decred/dcrd/blockchain/stake v1.1.0
	github.com/decred/dcrd/chaincfg v1.4.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/database v1.0.3
	github.com/decred/dcrd/dcrutil v1.2.1-0.20190118223730-3a5281156b73
	github.com/decred/dcrd/rpcclient/v2 v2.0.0
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrdata/api/types/v2 v2.0.1
	github.com/decred/dcrdata/rpcutils v1.1.0
	github.com/decred/dcrdata/txhelpers v1.1.0
	github.com/decred/slog v1.0.0
	github.com/dgraph-io/badger v1.5.5-0.20190214192501-3196cc1d7a5f
	github.com/dustin/go-humanize v1.0.0 // indirect
)
