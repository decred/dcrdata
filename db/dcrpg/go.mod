module github.com/decred/dcrdata/db/dcrpg

go 1.12

require (
	github.com/chappjc/trylock v1.0.0
	github.com/davecgh/go-spew v1.1.1
	github.com/decred/dcrd/blockchain/stake v1.1.0
	github.com/decred/dcrd/chaincfg v1.3.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrec v0.0.0-20190328014953-d8c19637977c // indirect
	github.com/decred/dcrd/dcrec/edwards v0.0.0-20190328014953-d8c19637977c // indirect
	github.com/decred/dcrd/dcrjson/v2 v2.0.0
	github.com/decred/dcrd/dcrutil v1.2.1-0.20190118223730-3a5281156b73
	github.com/decred/dcrd/rpcclient/v2 v2.0.0
	github.com/decred/dcrd/txscript v1.0.3-0.20190328014953-d8c19637977c
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrdata/api/types v1.0.6
	github.com/decred/dcrdata/blockdata v1.0.1
	github.com/decred/dcrdata/db/cache v1.0.1
	github.com/decred/dcrdata/db/dbtypes v1.0.1
	github.com/decred/dcrdata/rpcutils v1.0.1
	github.com/decred/dcrdata/semver v1.0.0
	github.com/decred/dcrdata/stakedb v1.0.1
	github.com/decred/dcrdata/txhelpers v1.0.1
	github.com/decred/slog v1.0.0
	github.com/dustin/go-humanize v1.0.0
	github.com/lib/pq v1.0.0
)

replace (
	github.com/decred/dcrdata/rpcutils => ../../rpcutils
	github.com/decred/dcrdata/txhelpers => ../../txhelpers
)
