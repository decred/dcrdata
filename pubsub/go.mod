module github.com/decred/dcrdata/pubsub/v4

go 1.12

require (
	github.com/decred/dcrd/chaincfg/v2 v2.3.0
	github.com/decred/dcrd/dcrutil/v2 v2.0.1
	github.com/decred/dcrd/rpc/jsonrpc/types/v2 v2.0.0
	github.com/decred/dcrd/txscript/v2 v2.1.0
	github.com/decred/dcrd/wire v1.3.0
	github.com/decred/dcrdata/blockdata/v5 v5.0.1
	github.com/decred/dcrdata/db/dbtypes/v2 v2.2.1
	github.com/decred/dcrdata/explorer/types/v2 v2.1.1
	github.com/decred/dcrdata/mempool/v5 v5.0.2
	github.com/decred/dcrdata/pubsub/types/v3 v3.0.5
	github.com/decred/dcrdata/semver v1.0.0
	github.com/decred/dcrdata/txhelpers/v4 v4.0.1
	github.com/decred/slog v1.0.0
	golang.org/x/net v0.0.0-20191028085509-fe3aa8a45271
)

replace github.com/decred/dcrdata/explorer/types/v2 => ../explorer/types
