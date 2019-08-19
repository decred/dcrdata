module github.com/decred/dcrdata/v5

require (
	github.com/caarlos0/env v3.5.0+incompatible
	github.com/chappjc/logrus-prefix v0.0.0-20180227015900-3a1d64819adb
	github.com/decred/dcrd/blockchain v1.2.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.2
	github.com/decred/dcrd/chaincfg/v2 v2.1.0
	github.com/decred/dcrd/dcrec v1.0.0
	github.com/decred/dcrd/dcrutil/v2 v2.0.0
	github.com/decred/dcrd/rpc/jsonrpc/types v1.0.0
	github.com/decred/dcrd/rpcclient/v4 v4.0.0
	github.com/decred/dcrd/txscript/v2 v2.0.0
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrdata/api/types/v4 v4.0.2
	github.com/decred/dcrdata/blockdata/v4 v4.0.3
	github.com/decred/dcrdata/db/cache/v2 v2.2.2
	github.com/decred/dcrdata/db/dbtypes/v2 v2.1.2
	github.com/decred/dcrdata/db/dcrpg/v4 v4.0.3
	github.com/decred/dcrdata/db/dcrsqlite/v4 v4.0.3
	github.com/decred/dcrdata/exchanges/v2 v2.0.2
	github.com/decred/dcrdata/explorer/types/v2 v2.0.2
	github.com/decred/dcrdata/gov/v2 v2.0.2
	github.com/decred/dcrdata/mempool/v4 v4.0.3
	github.com/decred/dcrdata/middleware/v3 v3.0.2
	github.com/decred/dcrdata/pubsub/types/v3 v3.0.2
	github.com/decred/dcrdata/pubsub/v3 v3.0.3
	github.com/decred/dcrdata/rpcutils/v2 v2.0.3
	github.com/decred/dcrdata/semver v1.0.0
	github.com/decred/dcrdata/stakedb/v3 v3.0.3
	github.com/decred/dcrdata/txhelpers/v3 v3.0.2
	github.com/decred/slog v1.0.0
	github.com/didip/tollbooth v4.0.1-0.20180415195142-b10a036da5f0+incompatible
	github.com/didip/tollbooth_chi v0.0.0-20170928041846-6ab5f3083f3d
	github.com/dmigwi/go-piparser/proposals v0.0.0-20190426030541-8412e0f44f55
	github.com/dustin/go-humanize v1.0.0
	github.com/go-chi/chi v4.0.3-0.20190316151245-d08916613452+incompatible
	github.com/google/gops v0.3.6
	github.com/googollee/go-engine.io v0.0.0-20180829091931-e2f255711dcb // indirect
	github.com/googollee/go-socket.io v0.0.0-20181214084611-0ad7206c347a
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	github.com/konsorten/go-windows-terminal-sequences v1.0.2 // indirect
	github.com/mattn/go-colorable v0.1.1 // indirect
	github.com/mgutz/ansi v0.0.0-20170206155736-9520e82c474b // indirect
	github.com/patrickmn/go-cache v2.1.0+incompatible // indirect
	github.com/rs/cors v1.6.0
	github.com/shiena/ansicolor v0.0.0-20151119151921-a422bbe96644
	github.com/sirupsen/logrus v1.3.0
	github.com/smartystreets/goconvey v0.0.0-20190330032615-68dc04aab96a // indirect
	github.com/x-cray/logrus-prefixed-formatter v0.5.2 // indirect
	golang.org/x/net v0.0.0-20190613194153-d28f0bde5980
	golang.org/x/time v0.0.0-20190308202827-9d24e82272b4 // indirect
)

replace (
	github.com/decred/dcrdata/api/types/v4 => ./api/types
	github.com/decred/dcrdata/blockdata/v4 => ./blockdata
	github.com/decred/dcrdata/db/cache/v2 => ./db/cache
	github.com/decred/dcrdata/db/dbtypes/v2 => ./db/dbtypes
	github.com/decred/dcrdata/db/dcrpg/v4 => ./db/dcrpg
	github.com/decred/dcrdata/db/dcrsqlite/v4 => ./db/dcrsqlite
	github.com/decred/dcrdata/dcrrates => ./dcrrates
	github.com/decred/dcrdata/exchanges/v2 => ./exchanges
	github.com/decred/dcrdata/explorer/types/v2 => ./explorer/types
	github.com/decred/dcrdata/gov/v2 => ./gov
	github.com/decred/dcrdata/mempool/v4 => ./mempool
	github.com/decred/dcrdata/middleware/v3 => ./middleware
	github.com/decred/dcrdata/pubsub/types/v3 => ./pubsub/types
	github.com/decred/dcrdata/pubsub/v3 => ./pubsub
	github.com/decred/dcrdata/rpcutils/v2 => ./rpcutils
	github.com/decred/dcrdata/semver => ./semver
	github.com/decred/dcrdata/stakedb/v3 => ./stakedb
	github.com/decred/dcrdata/testutil/dbconfig/v2 => ./testutil/dbconfig
	github.com/decred/dcrdata/txhelpers/v3 => ./txhelpers
)
