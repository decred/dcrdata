module github.com/decred/dcrdata/cmd/dcrdata

go 1.15

replace (
	github.com/decred/dcrdata/db/dcrpg/v6 => ../../db/dcrpg/
	github.com/decred/dcrdata/exchanges/v3 => ../../exchanges/
	github.com/decred/dcrdata/gov/v4 => ../../gov/
	github.com/decred/dcrdata/v6 => ../../
)

require (
	github.com/caarlos0/env/v6 v6.5.0
	github.com/decred/dcrd/blockchain/stake/v3 v3.0.0
	github.com/decred/dcrd/blockchain/standalone/v2 v2.0.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.3-0.20200921185235-6d75c7ec1199
	github.com/decred/dcrd/chaincfg/v3 v3.0.0
	github.com/decred/dcrd/dcrec v1.0.1-0.20200921185235-6d75c7ec1199
	github.com/decred/dcrd/dcrutil/v3 v3.0.0
	github.com/decred/dcrd/rpc/jsonrpc/types/v2 v2.3.0
	github.com/decred/dcrd/rpcclient/v6 v6.0.2
	github.com/decred/dcrd/txscript/v3 v3.0.0
	github.com/decred/dcrd/wire v1.4.0
	github.com/decred/dcrdata/db/dcrpg/v6 v6.0.0
	github.com/decred/dcrdata/exchanges/v3 v3.0.0
	github.com/decred/dcrdata/gov/v4 v4.0.0
	github.com/decred/dcrdata/v6 v6.0.0
	github.com/decred/politeia v1.0.1
	github.com/decred/slog v1.1.0
	github.com/didip/tollbooth/v6 v6.1.0
	github.com/dustin/go-humanize v1.0.0
	github.com/go-chi/chi/v5 v5.0.1
	github.com/go-chi/docgen v1.2.0
	github.com/google/gops v0.3.17
	github.com/googollee/go-socket.io v1.4.4
	github.com/jessevdk/go-flags v1.4.1-0.20200711081900-c17162fe8fd7
	github.com/jrick/logrotate v1.0.0
	github.com/rs/cors v1.7.1-0.20201213214713-f9bce55a4e61
	golang.org/x/net v0.0.0-20210119194325-5f4716e94777
)
