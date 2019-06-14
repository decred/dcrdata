module github.com/decred/dcrdata/db/dcrpg

go 1.11

require (
	github.com/chappjc/trylock v1.0.0
	github.com/davecgh/go-spew v1.1.1
	github.com/decred/dcrd/blockchain/stake v1.1.0
	github.com/decred/dcrd/chaincfg v1.5.1
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrjson/v2 v2.0.0
	github.com/decred/dcrd/dcrutil v1.3.0
	github.com/decred/dcrd/rpcclient/v2 v2.0.0
	github.com/decred/dcrd/txscript v1.0.3-0.20190613214542-d0a6bf024dfc
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrdata/api/types/v3 v3.0.0
	github.com/decred/dcrdata/blockdata v1.0.1
	github.com/decred/dcrdata/db/cache/v2 v2.0.0
	github.com/decred/dcrdata/db/dbtypes/v2 v2.0.0
	github.com/decred/dcrdata/rpcutils v1.0.2-0.20190416204615-70a58657e02f
	github.com/decred/dcrdata/semver v1.0.0
	github.com/decred/dcrdata/stakedb v1.0.1
	github.com/decred/dcrdata/testutil/dbconfig v1.0.1
	github.com/decred/dcrdata/txhelpers/v2 v2.0.0
	github.com/decred/slog v1.0.0
	github.com/dmigwi/go-piparser/proposals v0.0.0-20190426030541-8412e0f44f55
	github.com/dustin/go-humanize v1.0.0
	github.com/lib/pq v1.1.0
)
