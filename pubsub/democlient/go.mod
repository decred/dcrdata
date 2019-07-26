module github.com/decred/dcrdata/pubsub/democlient

replace (
	github.com/decred/dcrdata/api/types/v4 => ../../api/types
	github.com/decred/dcrdata/blockdata/v4 => ../../blockdata
	github.com/decred/dcrdata/db/cache/v2 => ../../db/cache
	github.com/decred/dcrdata/db/dbtypes/v2 => ../../db/dbtypes
	github.com/decred/dcrdata/db/dcrpg/v3 => ../../db/dcrpg
	github.com/decred/dcrdata/db/dcrsqlite/v3 => ../../db/dcrsqlite
	github.com/decred/dcrdata/dcrrates => ../../dcrrates
	github.com/decred/dcrdata/exchanges/v2 => ../../exchanges
	github.com/decred/dcrdata/explorer/types/v2 => ../../explorer/types
	github.com/decred/dcrdata/gov/v2 => ../../gov
	github.com/decred/dcrdata/mempool/v4 => ../../mempool
	github.com/decred/dcrdata/middleware/v3 => ../../middleware
	github.com/decred/dcrdata/pubsub/types/v3 => ../types
	github.com/decred/dcrdata/pubsub/v3 => ../
	github.com/decred/dcrdata/rpcutils/v2 => ../../rpcutils
	github.com/decred/dcrdata/semver => ../../semver
	github.com/decred/dcrdata/stakedb/v3 => ../../stakedb
	github.com/decred/dcrdata/testutil/dbconfig/v2 => ../../testutil/dbconfig
	github.com/decred/dcrdata/txhelpers/v3 => ../../txhelpers
)

require (
	github.com/decred/dcrd/chaincfg/v2 v2.0.2
	github.com/decred/dcrd/dcrutil/v2 v2.0.0
	github.com/decred/dcrdata/explorer/types/v2 v2.0.0
	github.com/decred/dcrdata/pubsub/types/v3 v3.0.0
	github.com/decred/dcrdata/pubsub/v3 v3.0.0
	github.com/decred/dcrdata/semver v1.0.0
	github.com/decred/slog v1.0.0
	github.com/google/go-cmp v0.3.0 // indirect
	github.com/jessevdk/go-flags v1.4.0
	github.com/kr/pty v1.1.4 // indirect
	go.etcd.io/bbolt v1.3.2 // indirect
	google.golang.org/genproto v0.0.0-20190502173448-54afdca5d873 // indirect
	google.golang.org/grpc v1.20.1 // indirect
	gopkg.in/AlecAivazis/survey.v1 v1.8.2
)
