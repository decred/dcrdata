module github.com/decred/dcrdata/gov/v6

go 1.18

replace github.com/decred/dcrdata/v8 => ../

require (
	github.com/asdine/storm/v3 v3.2.1
	github.com/decred/dcrd/chaincfg/v3 v3.2.0
	github.com/decred/dcrd/dcrjson/v4 v4.0.1
	github.com/decred/dcrd/rpc/jsonrpc/types/v4 v4.1.0
	github.com/decred/dcrdata/v8 v8.0.0
	github.com/decred/politeia v1.4.0
	github.com/decred/slog v1.2.0
)

require (
	decred.org/dcrwallet v1.7.0 // indirect
	github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412 // indirect
	github.com/dchest/siphash v1.2.3 // indirect
	github.com/decred/base58 v1.0.5 // indirect
	github.com/decred/dcrd/blockchain/stake/v3 v3.0.0 // indirect
	github.com/decred/dcrd/blockchain/stake/v5 v5.0.0 // indirect
	github.com/decred/dcrd/blockchain/standalone/v2 v2.2.0 // indirect
	github.com/decred/dcrd/certgen v1.1.1 // indirect
	github.com/decred/dcrd/chaincfg/chainhash v1.0.4 // indirect
	github.com/decred/dcrd/crypto/blake256 v1.0.1 // indirect
	github.com/decred/dcrd/crypto/ripemd160 v1.0.2 // indirect
	github.com/decred/dcrd/database/v2 v2.0.2 // indirect
	github.com/decred/dcrd/database/v3 v3.0.1 // indirect
	github.com/decred/dcrd/dcrec v1.0.1 // indirect
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.3 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v3 v3.0.0 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.2.0 // indirect
	github.com/decred/dcrd/dcrutil/v3 v3.0.0 // indirect
	github.com/decred/dcrd/dcrutil/v4 v4.0.1 // indirect
	github.com/decred/dcrd/gcs/v2 v2.1.0 // indirect
	github.com/decred/dcrd/hdkeychain/v3 v3.1.0 // indirect
	github.com/decred/dcrd/txscript/v3 v3.0.0 // indirect
	github.com/decred/dcrd/txscript/v4 v4.1.0 // indirect
	github.com/decred/dcrd/wire v1.6.0 // indirect
	github.com/decred/dcrtime v0.0.0-20191018193024-8d8b4ef0458e // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/google/trillian v1.4.1 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/gorilla/schema v1.1.0 // indirect
	github.com/h2non/go-is-svg v0.0.0-20160927212452-35e8c4b0612c // indirect
	github.com/klauspost/cpuid/v2 v2.0.9 // indirect
	github.com/marcopeereboom/sbox v1.1.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/syndtr/goleveldb v1.0.1-0.20210819022825-2ae1ddf74ef7 // indirect
	go.etcd.io/bbolt v1.3.6 // indirect
	golang.org/x/crypto v0.7.0 // indirect
	golang.org/x/net v0.8.0 // indirect
	golang.org/x/sys v0.6.0 // indirect
	lukechampine.com/blake3 v1.2.1 // indirect
)
