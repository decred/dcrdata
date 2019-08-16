module github.com/decred/dcrdata/middleware/v3

go 1.11

require (
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/chaincfg/v2 v2.1.0
	github.com/decred/dcrd/dcrutil/v2 v2.0.0
	github.com/decred/dcrd/rpc/jsonrpc/types v1.0.0
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrdata/api/types/v4 v4.0.2
	github.com/decred/slog v1.0.0
	github.com/didip/tollbooth/v5 v5.1.1-0.20190817151620-2c720dff9427
	github.com/go-chi/chi v4.0.3-0.20190807011452-43097498be03+incompatible
	github.com/go-chi/docgen v1.0.5
	github.com/patrickmn/go-cache v2.1.0+incompatible // indirect
	golang.org/x/time v0.0.0-20190308202827-9d24e82272b4 // indirect
)
