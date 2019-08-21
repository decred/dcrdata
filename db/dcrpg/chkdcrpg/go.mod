module github.com/decred/dcrdata/db/dcrpg/chkdcrpg

go 1.12

replace (
	github.com/decred/dcrdata/db/dcrpg/v4 => ../
	github.com/decred/dcrdata/stakedb/v3 => ../../../stakedb
)

require (
	github.com/decred/dcrd/chaincfg/v2 v2.2.0
	github.com/decred/dcrd/dcrutil/v2 v2.0.0
	github.com/decred/dcrd/rpcclient/v4 v4.0.0
	github.com/decred/dcrdata/db/dcrpg/v4 v4.0.3
	github.com/decred/dcrdata/rpcutils/v2 v2.0.3
	github.com/decred/dcrdata/stakedb/v3 v3.0.3
	github.com/decred/dcrdata/txhelpers/v3 v3.0.2
	github.com/decred/dcrdata/v5 v5.1.0
	github.com/decred/slog v1.0.0
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
)
