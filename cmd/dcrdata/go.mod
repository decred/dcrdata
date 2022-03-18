module github.com/decred/dcrdata/cmd/dcrdata

go 1.17

replace (
	github.com/decred/dcrdata/db/dcrpg/v7 => ../../db/dcrpg/
	github.com/decred/dcrdata/exchanges/v3 => ../../exchanges/
	github.com/decred/dcrdata/gov/v5 => ../../gov/
	github.com/decred/dcrdata/v7 => ../../
)

require (
	github.com/caarlos0/env/v6 v6.6.2
	github.com/decred/dcrd/blockchain/stake/v4 v4.0.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.3
	github.com/decred/dcrd/chaincfg/v3 v3.1.1
	github.com/decred/dcrd/dcrutil/v4 v4.0.0
	github.com/decred/dcrd/rpc/jsonrpc/types/v3 v3.0.0
	github.com/decred/dcrd/rpcclient/v7 v7.0.0
	github.com/decred/dcrd/txscript/v4 v4.0.0
	github.com/decred/dcrd/wire v1.5.0
	github.com/decred/dcrdata/db/dcrpg/v7 v7.0.0
	github.com/decred/dcrdata/exchanges/v3 v3.0.0
	github.com/decred/dcrdata/gov/v5 v5.0.0
	github.com/decred/dcrdata/v7 v7.0.0
	github.com/decred/politeia v1.3.0
	github.com/decred/slog v1.2.0
	github.com/didip/tollbooth/v6 v6.1.0
	github.com/dustin/go-humanize v1.0.1-0.20210705192016-249ff6c91207
	github.com/go-chi/chi/v5 v5.0.4
	github.com/go-chi/docgen v1.2.0
	github.com/google/gops v0.3.22
	github.com/googollee/go-socket.io v1.4.4
	github.com/jessevdk/go-flags v1.4.1-0.20200711081900-c17162fe8fd7
	github.com/jrick/logrotate v1.0.0
	github.com/rs/cors v1.8.0
	golang.org/x/net v0.0.0-20210903162142-ad29c8ab022f
	golang.org/x/text v0.3.6
)

require (
	decred.org/dcrdex v0.4.0 // indirect
	decred.org/dcrwallet v1.7.0 // indirect
	decred.org/dcrwallet/v2 v2.0.0 // indirect
	github.com/AndreasBriese/bbloom v0.0.0-20190825152654-46b345b51c96 // indirect
	github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412 // indirect
	github.com/asdine/storm/v3 v3.2.1 // indirect
	github.com/carterjones/go-cloudflare-scraper v0.1.2 // indirect
	github.com/carterjones/signalr v0.3.5 // indirect
	github.com/cespare/xxhash v1.1.0 // indirect
	github.com/dchest/siphash v1.2.2 // indirect
	github.com/decred/base58 v1.0.3 // indirect
	github.com/decred/dcrd/blockchain/stake/v3 v3.0.0 // indirect
	github.com/decred/dcrd/blockchain/standalone/v2 v2.1.0 // indirect
	github.com/decred/dcrd/certgen v1.1.1 // indirect
	github.com/decred/dcrd/crypto/blake256 v1.0.1-0.20200921185235-6d75c7ec1199 // indirect
	github.com/decred/dcrd/crypto/ripemd160 v1.0.1 // indirect
	github.com/decred/dcrd/database/v2 v2.0.2 // indirect
	github.com/decred/dcrd/database/v3 v3.0.0 // indirect
	github.com/decred/dcrd/dcrec v1.0.1-0.20200921185235-6d75c7ec1199 // indirect
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.2 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v3 v3.0.0 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.1 // indirect
	github.com/decred/dcrd/dcrjson/v4 v4.0.0 // indirect
	github.com/decred/dcrd/dcrutil/v3 v3.0.0 // indirect
	github.com/decred/dcrd/gcs/v2 v2.1.0 // indirect
	github.com/decred/dcrd/gcs/v3 v3.0.0 // indirect
	github.com/decred/dcrd/hdkeychain/v3 v3.1.0 // indirect
	github.com/decred/dcrd/txscript/v3 v3.0.0 // indirect
	github.com/decred/dcrtime v0.0.0-20191018193024-8d8b4ef0458e // indirect
	github.com/decred/go-socks v1.1.0 // indirect
	github.com/dgraph-io/badger v1.6.2 // indirect
	github.com/dgraph-io/ristretto v0.0.2 // indirect
	github.com/go-pkgz/expirable-cache v0.0.3 // indirect
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b // indirect
	github.com/golang/protobuf v1.4.3 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/google/trillian v1.3.13 // indirect
	github.com/google/uuid v1.1.5 // indirect
	github.com/gorilla/schema v1.1.0 // indirect
	github.com/gorilla/websocket v1.4.2 // indirect
	github.com/h2non/go-is-svg v0.0.0-20160927212452-35e8c4b0612c // indirect
	github.com/lib/pq v1.10.3 // indirect
	github.com/marcopeereboom/sbox v1.1.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/robertkrimen/otto v0.0.0-20180617131154-15f95af6e78d // indirect
	github.com/syndtr/goleveldb v1.0.1-0.20210819022825-2ae1ddf74ef7 // indirect
	go.etcd.io/bbolt v1.3.5 // indirect
	golang.org/x/crypto v0.0.0-20210711020723-a769d52b0f97 // indirect
	golang.org/x/sys v0.0.0-20210902050250-f475640dd07b // indirect
	golang.org/x/time v0.0.0-20210220033141-f8bda1e9f3ba // indirect
	google.golang.org/genproto v0.0.0-20201022181438-0ff5f38871d5 // indirect
	google.golang.org/grpc v1.36.1 // indirect
	google.golang.org/protobuf v1.25.0 // indirect
	gopkg.in/ini.v1 v1.55.0 // indirect
	gopkg.in/sourcemap.v1 v1.0.5 // indirect
)
