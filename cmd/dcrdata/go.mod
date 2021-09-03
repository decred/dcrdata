module github.com/decred/dcrdata/cmd/dcrdata

go 1.16

replace (
	github.com/decred/dcrdata/db/dcrpg/v7 => ../../db/dcrpg/
	github.com/decred/dcrdata/exchanges/v3 => ../../exchanges/
	github.com/decred/dcrdata/gov/v5 => ../../gov/
	github.com/decred/dcrdata/v7 => ../../
)

require (
	github.com/caarlos0/env/v6 v6.6.2
	github.com/decred/dcrd/blockchain/stake/v4 v4.0.0-20210901152745-8830d9c9cdba
	github.com/decred/dcrd/blockchain/standalone/v2 v2.0.1-0.20210525214639-70483c835b7f
	github.com/decred/dcrd/chaincfg/chainhash v1.0.3-0.20210525214639-70483c835b7f
	github.com/decred/dcrd/chaincfg/v3 v3.0.1-0.20210525214639-70483c835b7f
	github.com/decred/dcrd/dcrutil/v4 v4.0.0-20210901152745-8830d9c9cdba
	github.com/decred/dcrd/rpc/jsonrpc/types/v3 v3.0.0-20210901152745-8830d9c9cdba
	github.com/decred/dcrd/rpcclient/v7 v7.0.0-20210901152745-8830d9c9cdba
	github.com/decred/dcrd/txscript/v4 v4.0.0-20210901152745-8830d9c9cdba
	github.com/decred/dcrd/wire v1.4.1-0.20210727015103-4d81fc1b6e95
	github.com/decred/dcrdata/db/dcrpg/v7 v7.0.0
	github.com/decred/dcrdata/exchanges/v3 v3.0.0
	github.com/decred/dcrdata/gov/v5 v5.0.0
	github.com/decred/dcrdata/v7 v7.0.0
	github.com/decred/politeia v1.1.0
	github.com/decred/slog v1.2.0
	github.com/didip/tollbooth/v6 v6.1.0
	github.com/dustin/go-humanize v1.0.1-0.20210705192016-249ff6c91207
	github.com/go-chi/chi/v5 v5.0.4
	github.com/go-chi/docgen v1.2.0
	github.com/google/gops v0.3.17
	github.com/googollee/go-socket.io v1.4.4
	github.com/jessevdk/go-flags v1.4.1-0.20200711081900-c17162fe8fd7
	github.com/jrick/logrotate v1.0.0
	github.com/rs/cors v1.8.0
	golang.org/x/net v0.0.0-20210903162142-ad29c8ab022f
)
