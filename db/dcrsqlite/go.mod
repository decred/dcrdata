module github.com/decred/dcrdata/db/dcrsqlite/v2

go 1.11

replace github.com/decred/dcrdata/db/dcrpg => ../dcrpg

require (
	github.com/decred/dcrd/blockchain/stake v1.1.0
	github.com/decred/dcrd/chaincfg v1.4.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrjson/v2 v2.0.0
	github.com/decred/dcrd/dcrutil v1.2.1-0.20190118223730-3a5281156b73
	github.com/decred/dcrd/rpcclient/v2 v2.0.0
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrdata/api/types/v2 v2.0.1
	github.com/decred/dcrdata/blockdata v1.0.2
	github.com/decred/dcrdata/db/dbtypes v1.1.0
	github.com/decred/dcrdata/explorer/types v1.0.1
	github.com/decred/dcrdata/mempool/v2 v2.0.0
	github.com/decred/dcrdata/rpcutils v1.1.0
	github.com/decred/dcrdata/stakedb v1.0.2
	github.com/decred/dcrdata/txhelpers v1.1.0
	github.com/decred/slog v1.0.0
	github.com/dustin/go-humanize v1.0.0
	github.com/google/go-cmp v0.2.0
	github.com/mattn/go-sqlite3 v1.10.0
	golang.org/x/crypto v0.0.0-20190403202508-8e1b8d32e692 // indirect
	golang.org/x/net v0.0.0-20190403144856-b630fd6fe46b // indirect
)
