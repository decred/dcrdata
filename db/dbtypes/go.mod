module github.com/decred/dcrdata/db/dbtypes

go 1.11

replace github.com/decred/dcrdata/db/dcrpg => ../dcrpg

require (
	github.com/DataDog/zstd v1.4.0 // indirect
	github.com/decred/dcrd/blockchain/stake v1.1.0
	github.com/decred/dcrd/chaincfg v1.4.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrutil v1.2.1-0.20190118223730-3a5281156b73
	github.com/decred/dcrd/txscript v1.0.3-0.20190402182842-879eebce3333
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrdata/txhelpers v1.1.0
	github.com/dgryski/go-farm v0.0.0-20190416075124-e1214b5e05dc // indirect
	github.com/lib/pq v1.1.0 // indirect
	golang.org/x/text v0.3.1-0.20180807135948-17ff2d5776d2 // indirect
)
