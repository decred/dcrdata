module github.com/decred/dcrdata/db/dcrpg/v8

go 1.18

replace github.com/decred/dcrdata/v8 => ../../

require (
	github.com/davecgh/go-spew v1.1.1
	github.com/decred/dcrd/blockchain/stake/v5 v5.0.0
	github.com/decred/dcrd/blockchain/standalone/v2 v2.2.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.4
	github.com/decred/dcrd/chaincfg/v3 v3.2.0
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.2.0
	github.com/decred/dcrd/dcrutil/v4 v4.0.1
	github.com/decred/dcrd/rpc/jsonrpc/types/v4 v4.1.0
	github.com/decred/dcrd/rpcclient/v8 v8.0.0
	github.com/decred/dcrd/txscript/v4 v4.1.0
	github.com/decred/dcrd/wire v1.6.0
	github.com/decred/dcrdata/v8 v8.0.0
	github.com/decred/slog v1.2.0
	github.com/dustin/go-humanize v1.0.1
	github.com/jessevdk/go-flags v1.5.0
	github.com/jrick/logrotate v1.0.0
	github.com/lib/pq v1.10.4
)

require (
	github.com/AndreasBriese/bbloom v0.0.0-20190825152654-46b345b51c96 // indirect
	github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412 // indirect
	github.com/cespare/xxhash v1.1.0 // indirect
	github.com/dchest/siphash v1.2.3 // indirect
	github.com/decred/base58 v1.0.5 // indirect
	github.com/decred/dcrd/crypto/blake256 v1.0.1 // indirect
	github.com/decred/dcrd/crypto/ripemd160 v1.0.2 // indirect
	github.com/decred/dcrd/database/v3 v3.0.1 // indirect
	github.com/decred/dcrd/dcrec v1.0.1 // indirect
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.3 // indirect
	github.com/decred/dcrd/dcrjson/v4 v4.0.1 // indirect
	github.com/decred/dcrd/gcs/v4 v4.0.0 // indirect
	github.com/decred/go-socks v1.1.0 // indirect
	github.com/dgraph-io/badger v1.6.2 // indirect
	github.com/dgraph-io/ristretto v0.0.2 // indirect
	github.com/golang/protobuf v1.4.3 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/gorilla/websocket v1.4.2 // indirect
	github.com/klauspost/cpuid/v2 v2.0.9 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/syndtr/goleveldb v1.0.1-0.20210819022825-2ae1ddf74ef7 // indirect
	golang.org/x/net v0.8.0 // indirect
	golang.org/x/sys v0.6.0 // indirect
	google.golang.org/protobuf v1.25.0 // indirect
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
	lukechampine.com/blake3 v1.2.1 // indirect
)
