module github.com/decred/dcrdata/pubsub/democlient

replace (
	github.com/decred/dcrdata/api/types => ../../api/types
	github.com/decred/dcrdata/blockdata => ../../blockdata
	github.com/decred/dcrdata/db/cache => ../../db/cache
	github.com/decred/dcrdata/db/dbtypes => ../../db/dbtypes
	github.com/decred/dcrdata/db/dcrpg => ../../db/dcrpg
	github.com/decred/dcrdata/db/dcrsqlite => ../../db/dcrsqlite
	github.com/decred/dcrdata/dcrrates => ../../dcrrates
	github.com/decred/dcrdata/exchanges => ../../exchanges
	github.com/decred/dcrdata/explorer/types => ../../explorer/types
	github.com/decred/dcrdata/gov/agendas => ../../gov/agendas
	github.com/decred/dcrdata/gov/politeia => ../../gov/politeia
	github.com/decred/dcrdata/mempool => ../../mempool
	github.com/decred/dcrdata/middleware => ../../middleware
	github.com/decred/dcrdata/pubsub => ../
	github.com/decred/dcrdata/pubsub/types => ../types
	github.com/decred/dcrdata/rpcutils => ../../rpcutils
	github.com/decred/dcrdata/semver => ../../semver
	github.com/decred/dcrdata/stakedb => ../../stakedb
	github.com/decred/dcrdata/testutil/dbconfig => ../../testutil/dbconfig
	github.com/decred/dcrdata/txhelpers => ../../txhelpers
)

require (
	github.com/decred/dcrd/dcrec v0.0.0-20190429225806-70c14042d837 // indirect
	github.com/decred/dcrd/dcrec/edwards v0.0.0-20190429225806-70c14042d837 // indirect
	github.com/decred/dcrd/dcrutil v1.2.1-0.20190118223730-3a5281156b73
	github.com/decred/dcrdata/explorer/types v1.0.1-0.20190501025527-02d6f8e648f7
	github.com/decred/dcrdata/pubsub v1.0.1-0.20190501025527-02d6f8e648f7
	github.com/decred/dcrdata/pubsub/types v1.0.1-0.20190501025527-02d6f8e648f7
	github.com/decred/dcrdata/txhelpers v1.0.2-0.20190501025527-02d6f8e648f7 // indirect
	github.com/google/go-cmp v0.3.0 // indirect
	github.com/jessevdk/go-flags v1.4.0
	github.com/kr/pty v1.1.4 // indirect
	golang.org/x/crypto v0.0.0-20190426145343-a29dc8fdc734 // indirect
	golang.org/x/net v0.0.0-20190502183928-7f726cade0ab
	golang.org/x/sys v0.0.0-20190502175342-a43fa875dd82 // indirect
	golang.org/x/text v0.3.2 // indirect
	google.golang.org/genproto v0.0.0-20190502173448-54afdca5d873 // indirect
	google.golang.org/grpc v1.20.1 // indirect
	gopkg.in/AlecAivazis/survey.v1 v1.8.2
)
