module github.com/decred/dcrdata/gov/v4

go 1.15

replace github.com/decred/dcrdata/v6 => ../

require (
	github.com/asdine/storm/v3 v3.2.1
	github.com/decred/dcrd/chaincfg/v3 v3.0.0
	github.com/decred/dcrd/dcrjson/v3 v3.1.0
	github.com/decred/dcrd/rpc/jsonrpc/types/v2 v2.3.0
	github.com/decred/dcrdata/v6 v6.0.0-20210510222533-6a2ca18d4382
	github.com/decred/politeia v1.0.1
	github.com/decred/slog v1.1.0
)
