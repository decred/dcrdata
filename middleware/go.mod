module github.com/decred/dcrdata/middleware/v2

go 1.11

replace github.com/decred/dcrdata/db/dcrpg => ../db/dcrpg

require (
	github.com/decred/dcrd/chaincfg v1.4.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrjson/v2 v2.0.0
	github.com/decred/dcrd/dcrutil v1.2.1-0.20190118223730-3a5281156b73
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrdata/api/types/v2 v2.0.1
	github.com/decred/slog v1.0.0
	github.com/go-chi/chi v4.0.3-0.20190316151245-d08916613452+incompatible
	github.com/go-chi/docgen v1.0.5
)
