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
	github.com/decred/dcrd/dcrutil v1.2.1-0.20190118223730-3a5281156b73
	github.com/decred/dcrdata/explorer/types v1.0.1-0.20190416204615-70a58657e02f
	github.com/decred/dcrdata/pubsub v1.0.1-0.20190416214715-6f7f00b15252
	github.com/decred/dcrdata/pubsub/types v1.0.1-0.20190416204615-70a58657e02f
	github.com/jessevdk/go-flags v1.4.0
	golang.org/x/net v0.0.0-20190415214537-1da14a5a36f2
	gopkg.in/AlecAivazis/survey.v1 v1.8.2
)
