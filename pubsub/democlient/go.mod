module github.com/decred/dcrdata/pubsub/democlient

go 1.18

replace github.com/decred/dcrdata/v8 => ../../

require (
	github.com/decred/dcrd/chaincfg/v3 v3.1.1
	github.com/decred/dcrd/txscript/v4 v4.0.0
	github.com/decred/dcrdata/v8 v8.0.0
	github.com/decred/slog v1.2.0
	github.com/jessevdk/go-flags v1.4.1-0.20200711081900-c17162fe8fd7
	gopkg.in/AlecAivazis/survey.v1 v1.8.2
)

require (
	github.com/AndreasBriese/bbloom v0.0.0-20190825152654-46b345b51c96 // indirect
	github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412 // indirect
	github.com/cespare/xxhash v1.1.0 // indirect
	github.com/dchest/siphash v1.2.2 // indirect
	github.com/decred/base58 v1.0.4 // indirect
	github.com/decred/dcrd/blockchain/stake/v4 v4.0.0 // indirect
	github.com/decred/dcrd/blockchain/standalone/v2 v2.1.0 // indirect
	github.com/decred/dcrd/chaincfg/chainhash v1.0.3 // indirect
	github.com/decred/dcrd/crypto/blake256 v1.0.1-0.20200921185235-6d75c7ec1199 // indirect
	github.com/decred/dcrd/crypto/ripemd160 v1.0.1 // indirect
	github.com/decred/dcrd/database/v3 v3.0.0 // indirect
	github.com/decred/dcrd/dcrec v1.0.1-0.20200921185235-6d75c7ec1199 // indirect
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.2 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.1 // indirect
	github.com/decred/dcrd/dcrjson/v4 v4.0.0 // indirect
	github.com/decred/dcrd/dcrutil/v4 v4.0.0 // indirect
	github.com/decred/dcrd/rpc/jsonrpc/types/v3 v3.0.0 // indirect
	github.com/decred/dcrd/wire v1.5.0 // indirect
	github.com/dgraph-io/badger v1.6.2 // indirect
	github.com/dgraph-io/ristretto v0.0.2 // indirect
	github.com/dustin/go-humanize v1.0.1-0.20210705192016-249ff6c91207 // indirect
	github.com/golang/protobuf v1.4.3 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51 // indirect
	github.com/kr/pty v1.1.2 // indirect
	github.com/mattn/go-colorable v0.1.8 // indirect
	github.com/mattn/go-isatty v0.0.12 // indirect
	github.com/mgutz/ansi v0.0.0-20170206155736-9520e82c474b // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/syndtr/goleveldb v1.0.1-0.20210819022825-2ae1ddf74ef7 // indirect
	golang.org/x/net v0.0.0-20210903162142-ad29c8ab022f // indirect
	golang.org/x/sys v0.0.0-20210902050250-f475640dd07b // indirect
	google.golang.org/protobuf v1.25.0 // indirect
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
)
