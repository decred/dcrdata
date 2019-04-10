module github.com/decred/dcrdata/db/dcrpg

go 1.12

require (
	github.com/Sereal/Sereal v0.0.0-20190226181601-237c2cca198f // indirect
	github.com/chappjc/trylock v1.0.0
	github.com/davecgh/go-spew v1.1.1
	github.com/decred/dcrd/blockchain/stake v1.1.0
	github.com/decred/dcrd/chaincfg v1.4.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrjson/v2 v2.0.0
	github.com/decred/dcrd/dcrutil v1.2.1-0.20190118223730-3a5281156b73
	github.com/decred/dcrd/rpcclient/v2 v2.0.0
	github.com/decred/dcrd/txscript v1.0.3-0.20190402182842-879eebce3333
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrdata/api/types v1.0.6
	github.com/decred/dcrdata/blockdata v1.0.1
	github.com/decred/dcrdata/db/cache v1.0.1
	github.com/decred/dcrdata/db/dbtypes v1.0.2-0.20190402170540-10fdc522fdb0
	github.com/decred/dcrdata/rpcutils v1.0.1
	github.com/decred/dcrdata/semver v1.0.0
	github.com/decred/dcrdata/stakedb v1.0.1
	github.com/decred/dcrdata/txhelpers v1.0.1
	github.com/decred/slog v1.0.0
	github.com/dustin/go-humanize v1.0.0
	github.com/golang/protobuf v1.3.1 // indirect
	github.com/golang/snappy v0.0.1 // indirect
	github.com/lib/pq v1.0.0
	github.com/onsi/ginkgo v1.8.0 // indirect
	github.com/onsi/gomega v1.5.0 // indirect
	github.com/stretchr/testify v1.3.0 // indirect
	github.com/vmihailenco/msgpack v4.0.4+incompatible // indirect
	golang.org/x/net v0.0.0-20190328230028-74de082e2cca // indirect
)

replace (
	github.com/decred/dcrdata/db/dbtypes => ../dbtypes
	github.com/decred/dcrdata/rpcutils => ../../rpcutils
	github.com/decred/dcrdata/txhelpers => ../../txhelpers
)
