module github.com/decred/dcrdata/gov/v2

go 1.12

require (
	github.com/asdine/storm/v3 v3.0.0-20191014171123-c370e07ad6d4
	github.com/decred/dcrd/chaincfg/v2 v2.2.0
	github.com/decred/dcrd/rpc/jsonrpc/types v1.0.0
	github.com/decred/dcrdata/db/dbtypes/v2 v2.1.2
	github.com/decred/dcrdata/semver v1.0.0
	github.com/decred/politeia v0.0.0-20190905145756-fa04479ca3eb
	github.com/decred/slog v1.0.0
	go.etcd.io/bbolt v1.3.3 // indirect
	golang.org/x/sys v0.0.0-20191010194322-b09406accb47 // indirect
)

replace github.com/golang/lint => golang.org/x/lint v0.0.0-20190301231843-5614ed5bae6f

replace sourcegraph.com/sourcegraph/go-diff => github.com/sourcegraph/go-diff v0.5.1
