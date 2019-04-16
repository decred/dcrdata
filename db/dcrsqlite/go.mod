module github.com/decred/dcrdata/db/dcrsqlite

go 1.12

replace github.com/decred/dcrdata/stakedb => ../../stakedb

require (
	github.com/Sereal/Sereal v0.0.0-20190226181601-237c2cca198f // indirect
	github.com/boltdb/bolt v1.3.1 // indirect
	github.com/decred/dcrd/blockchain/stake v1.1.0
	github.com/decred/dcrd/chaincfg v1.4.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrjson/v2 v2.0.0
	github.com/decred/dcrd/dcrutil v1.2.1-0.20190118223730-3a5281156b73
	github.com/decred/dcrd/rpcclient/v2 v2.0.0
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrdata/api/types v1.0.7-0.20190416163815-b92d2b40c258
	github.com/decred/dcrdata/blockdata v1.0.1
	github.com/decred/dcrdata/db/dbtypes v1.0.2-0.20190416163815-b92d2b40c258
	github.com/decred/dcrdata/explorer/types v1.0.1-0.20190416163815-b92d2b40c258
	github.com/decred/dcrdata/mempool v1.0.0
	github.com/decred/dcrdata/rpcutils v1.0.2-0.20190416163815-b92d2b40c258
	github.com/decred/dcrdata/stakedb v1.0.1
	github.com/decred/dcrdata/txhelpers v1.0.2-0.20190416163815-b92d2b40c258
	github.com/decred/slog v1.0.0
	github.com/dustin/go-humanize v1.0.0
	github.com/golang/snappy v0.0.1 // indirect
	github.com/google/go-cmp v0.2.0
	github.com/mattn/go-sqlite3 v1.10.0
	github.com/stretchr/testify v1.3.0 // indirect
	github.com/vmihailenco/msgpack v4.0.4+incompatible // indirect
)
