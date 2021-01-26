module github.com/decred/dcrdata/gov/v4

go 1.14

replace github.com/decred/dcrdata/v6 => ../

require (
	github.com/asdine/storm/v3 v3.2.1
	github.com/decred/dcrd/chaincfg/v3 v3.0.0
	github.com/decred/dcrd/dcrjson/v3 v3.1.0
	github.com/decred/dcrd/rpc/jsonrpc/types/v2 v2.3.0
	github.com/decred/dcrdata/v6 v6.0.0
	github.com/decred/politeia v0.0.0-20191031182202-b33af07598f2
	github.com/decred/slog v1.1.0
)
