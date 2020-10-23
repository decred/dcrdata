module github.com/decred/dcrdata/pkgs/stakingreward

go 1.13

require (
	github.com/decred/dcrd/chaincfg/v2 v2.3.0
	github.com/decred/dcrd/dcrutil v1.4.0
	github.com/decred/dcrd/wire v1.3.0
	github.com/decred/dcrdata/blockdata/v5 v5.0.1
	github.com/decred/dcrdata/exchanges/v2 v2.1.0
	github.com/decred/dcrdata/rpcutils/v3 v3.0.1
	github.com/decred/dcrdata/txhelpers/v4 v4.0.1
	github.com/decred/dcrdata/v5 v5.2.2
	github.com/decred/slog v1.0.0
	github.com/go-chi/chi v4.1.2+incompatible
)

replace github.com/decred/dcrdata/v5 => ../../
