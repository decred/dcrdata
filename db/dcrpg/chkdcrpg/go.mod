module github.com/decred/dcrdata/db/dcrpg/chkdcrpg

go 1.12

replace (
	github.com/decred/dcrdata/v5 => ../../..
	github.com/decred/dcrdata/txhelpers/v4 => ../../txhelpers
)

require (
	github.com/decred/dcrd/chaincfg/v2 v2.3.0
	github.com/decred/dcrd/dcrutil/v2 v2.0.1
	github.com/decred/dcrd/rpcclient/v5 v5.0.0
	github.com/decred/dcrdata/db/dcrpg/v5 v5.0.1
	github.com/decred/dcrdata/rpcutils/v3 v3.0.1
	github.com/decred/dcrdata/stakedb/v3 v3.1.1
	github.com/decred/dcrdata/txhelpers/v4 v4.0.1
	github.com/decred/dcrdata/v5 v5.1.1-0.20191031183729-78e26ce5fc81
	github.com/decred/slog v1.0.0
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
)
