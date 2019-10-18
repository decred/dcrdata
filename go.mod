module github.com/decred/dcrdata/v5

go 1.12

replace (
	github.com/decred/dcrdata/pubsub/types/v3 => ./pubsub/types
	github.com/decred/dcrdata/pubsub/v3 => ./pubsub
	github.com/go-critic/go-critic v0.0.0-20181204210945-0af0999fabfb => github.com/go-critic/go-critic v0.3.5-0.20190108192714-0af0999fabfb
	github.com/golangci/errcheck v0.0.0-20181003203344-ef45e06d44b6 => github.com/golangci/errcheck v0.0.0-20181223084120-ef45e06d44b6
	github.com/golangci/go-tools v0.0.0-20180109140146-35a9f45a5db0 => github.com/golangci/go-tools v0.0.0-20190124090046-35a9f45a5db0
	github.com/golangci/gofmt v0.0.0-20181105071733-0b8337e80d98 => github.com/golangci/gofmt v0.0.0-20181222123516-0b8337e80d98
)

require (
	github.com/caarlos0/env v3.5.0+incompatible
	github.com/chappjc/logrus-prefix v0.0.0-20180227015900-3a1d64819adb
	github.com/decred/dcrd/blockchain/standalone v1.0.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.2
	github.com/decred/dcrd/chaincfg/v2 v2.2.0
	github.com/decred/dcrd/dcrec v1.0.0
	github.com/decred/dcrd/dcrutil/v2 v2.0.0
	github.com/decred/dcrd/rpc/jsonrpc/types v1.0.0
	github.com/decred/dcrd/rpcclient/v4 v4.0.0
	github.com/decred/dcrd/txscript/v2 v2.0.0
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrdata/api/types/v4 v4.0.4
	github.com/decred/dcrdata/blockdata/v4 v4.0.5
	github.com/decred/dcrdata/db/cache/v2 v2.2.4
	github.com/decred/dcrdata/db/dbtypes/v2 v2.1.4
	github.com/decred/dcrdata/db/dcrpg/v4 v4.0.5
	github.com/decred/dcrdata/db/dcrsqlite/v4 v4.0.7
	github.com/decred/dcrdata/exchanges/v2 v2.0.3
	github.com/decred/dcrdata/explorer/types/v2 v2.0.4
	github.com/decred/dcrdata/gov/v2 v2.0.4
	github.com/decred/dcrdata/mempool/v4 v4.0.7
	github.com/decred/dcrdata/middleware/v3 v3.0.4
	github.com/decred/dcrdata/pubsub/types/v3 v3.0.4
	github.com/decred/dcrdata/pubsub/v3 v3.0.6
	github.com/decred/dcrdata/rpcutils/v2 v2.0.5
	github.com/decred/dcrdata/semver v1.0.0
	github.com/decred/dcrdata/stakedb/v3 v3.0.5
	github.com/decred/dcrdata/txhelpers/v3 v3.0.5
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
