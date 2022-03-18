module github.com/decred/dcrdata/exchanges/rateserver

go 1.17

replace github.com/decred/dcrdata/exchanges/v3 => ../

require (
	github.com/decred/dcrd/certgen v1.1.1
	github.com/decred/dcrd/dcrutil/v4 v4.0.0
	github.com/decred/dcrdata/exchanges/v3 v3.0.0
	github.com/decred/slog v1.2.0
	github.com/jessevdk/go-flags v1.4.1-0.20200711081900-c17162fe8fd7
	github.com/jrick/logrotate v1.0.0
	google.golang.org/grpc v1.36.1
)

require (
	decred.org/dcrdex v0.4.0 // indirect
	github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412 // indirect
	github.com/carterjones/go-cloudflare-scraper v0.1.2 // indirect
	github.com/carterjones/signalr v0.3.5 // indirect
	github.com/dchest/siphash v1.2.2 // indirect
	github.com/decred/base58 v1.0.3 // indirect
	github.com/decred/dcrd/chaincfg/chainhash v1.0.3 // indirect
	github.com/decred/dcrd/crypto/blake256 v1.0.1-0.20200921185235-6d75c7ec1199 // indirect
	github.com/decred/dcrd/crypto/ripemd160 v1.0.1 // indirect
	github.com/decred/dcrd/dcrec v1.0.1-0.20200921185235-6d75c7ec1199 // indirect
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.2 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.1 // indirect
	github.com/decred/dcrd/hdkeychain/v3 v3.1.0 // indirect
	github.com/decred/dcrd/txscript/v4 v4.0.0 // indirect
	github.com/decred/dcrd/wire v1.5.0 // indirect
	github.com/decred/go-socks v1.1.0 // indirect
	github.com/go-chi/chi/v5 v5.0.4 // indirect
	github.com/golang/protobuf v1.4.3 // indirect
	github.com/gorilla/websocket v1.4.2 // indirect
	github.com/lib/pq v1.10.3 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/robertkrimen/otto v0.0.0-20180617131154-15f95af6e78d // indirect
	go.etcd.io/bbolt v1.3.5 // indirect
	golang.org/x/crypto v0.0.0-20210711020723-a769d52b0f97 // indirect
	golang.org/x/net v0.0.0-20210903162142-ad29c8ab022f // indirect
	golang.org/x/sys v0.0.0-20210902050250-f475640dd07b // indirect
	golang.org/x/text v0.3.6 // indirect
	golang.org/x/time v0.0.0-20210220033141-f8bda1e9f3ba // indirect
	google.golang.org/genproto v0.0.0-20201022181438-0ff5f38871d5 // indirect
	google.golang.org/protobuf v1.25.0 // indirect
	gopkg.in/ini.v1 v1.55.0 // indirect
	gopkg.in/sourcemap.v1 v1.0.5 // indirect
)
