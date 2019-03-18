module github.com/decred/dcrdata/v4

require (
	github.com/caarlos0/env v3.5.0+incompatible
	github.com/chappjc/logrus-prefix v0.0.0-20180227015900-3a1d64819adb
	github.com/decred/dcrd/blockchain v1.1.1
	github.com/decred/dcrd/chaincfg v1.3.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrec v0.0.0-20190130161649-59ed4247a1d5
	github.com/decred/dcrd/dcrjson/v2 v2.0.0
	github.com/decred/dcrd/dcrutil v1.2.1-0.20190118223730-3a5281156b73
	github.com/decred/dcrd/rpcclient/v2 v2.0.0
	github.com/decred/dcrd/txscript v1.0.2
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrdata/api/types v1.0.6
	github.com/decred/dcrdata/blockdata v1.0.1
	github.com/decred/dcrdata/db/dbtypes v1.0.1
	github.com/decred/dcrdata/db/dcrpg v1.0.0
	github.com/decred/dcrdata/db/dcrsqlite v1.0.0
	github.com/decred/dcrdata/exchanges v1.0.0
	github.com/decred/dcrdata/explorer/types v1.0.0
	github.com/decred/dcrdata/gov/agendas v1.0.0
	github.com/decred/dcrdata/gov/politeia v1.0.0
	github.com/decred/dcrdata/mempool v1.0.0
	github.com/decred/dcrdata/middleware v1.0.1
	github.com/decred/dcrdata/pubsub v1.0.0
	github.com/decred/dcrdata/pubsub/types v1.0.0
	github.com/decred/dcrdata/rpcutils v1.0.1
	github.com/decred/dcrdata/semver v1.0.0
	github.com/decred/dcrdata/stakedb v1.0.1
	github.com/decred/dcrdata/txhelpers v1.0.1
	github.com/decred/dcrwallet/wallet v1.2.0
	github.com/decred/slog v1.0.0
	github.com/didip/tollbooth v4.0.1-0.20180415195142-b10a036da5f0+incompatible
	github.com/didip/tollbooth_chi v0.0.0-20170928041846-6ab5f3083f3d
	github.com/dustin/go-humanize v1.0.0
	github.com/go-chi/chi v4.0.3-0.20190316151245-d08916613452+incompatible
<<<<<<< HEAD
<<<<<<< HEAD
=======
=======
	github.com/gchaincl/dotsql v0.1.0
	github.com/go-chi/chi v4.0.1+incompatible
>>>>>>> 8f49aa5... Add the sqlite tests and the dump data loading
=======
>>>>>>> 769fa89... Make underlying chartsdata pointers as lean as possible
	github.com/go-chi/docgen v1.0.5
	github.com/golang/protobuf v1.2.0
	github.com/google/go-cmp v0.2.0
>>>>>>> 55b9d8c... Add the sqlite tests and the dump data loading
	github.com/google/gops v0.3.6
	github.com/googollee/go-socket.io v0.0.0-20181214084611-0ad7206c347a
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	github.com/rs/cors v1.6.0
	github.com/shiena/ansicolor v0.0.0-20151119151921-a422bbe96644
	github.com/sirupsen/logrus v1.2.0
<<<<<<< HEAD
	golang.org/x/net v0.0.0-20190326090315-15845e8f865b
)

replace (
	github.com/decred/dcrdata/api/types => ./api/types
	github.com/decred/dcrdata/blockdata => ./blockdata
	github.com/decred/dcrdata/db/cache => ./db/cache
	github.com/decred/dcrdata/db/dbtypes => ./db/dbtypes
	github.com/decred/dcrdata/db/dcrpg => ./db/dcrpg
	github.com/decred/dcrdata/db/dcrsqlite => ./db/dcrsqlite
	github.com/decred/dcrdata/dcrrates => ./dcrrates
	github.com/decred/dcrdata/exchanges => ./exchanges
	github.com/decred/dcrdata/explorer/types => ./explorer/types
	github.com/decred/dcrdata/gov/agendas => ./gov/agendas
	github.com/decred/dcrdata/gov/politeia => ./gov/politeia
	github.com/decred/dcrdata/mempool => ./mempool
	github.com/decred/dcrdata/middleware => ./middleware
	github.com/decred/dcrdata/pubsub => ./pubsub
	github.com/decred/dcrdata/pubsub/types => ./pubsub/types
	github.com/decred/dcrdata/rpcutils => ./rpcutils
	github.com/decred/dcrdata/semver => ./semver
	github.com/decred/dcrdata/stakedb => ./stakedb
	github.com/decred/dcrdata/txhelpers => ./txhelpers
=======
	github.com/smartystreets/assertions v0.0.0-20180927180507-b2de0cb4f26d // indirect
	github.com/smartystreets/goconvey v0.0.0-20181108003508-044398e4856c // indirect
	github.com/x-cray/logrus-prefixed-formatter v0.5.2 // indirect
	golang.org/x/net v0.0.0-20190213061140-3a22650c66bd
	golang.org/x/time v0.0.0-20181108054448-85acf8d2951c // indirect
	google.golang.org/appengine v1.3.0 // indirect
	google.golang.org/genproto v0.0.0-20190219182410-082222b4a5c5 // indirect
	google.golang.org/grpc v1.18.0
<<<<<<< HEAD
	mellium.im/sasl v0.2.1 // indirect
>>>>>>> 55b9d8c... Add the sqlite tests and the dump data loading
=======
>>>>>>> 769fa89... Make underlying chartsdata pointers as lean as possible
)
