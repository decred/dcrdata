module github.com/decred/dcrdata/exchanges/rateserver

go 1.16

replace github.com/decred/dcrdata/exchanges/v3 => ../

require (
	github.com/decred/dcrd/certgen v1.1.1
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.0-20210901152745-8830d9c9cdba // indirect
	github.com/decred/dcrd/dcrutil/v4 v4.0.0-20210925154931-7b184ab3fd61
	github.com/decred/dcrd/txscript/v4 v4.0.0-20210901152745-8830d9c9cdba // indirect
	github.com/decred/dcrdata/exchanges/v3 v3.0.0
	github.com/decred/slog v1.2.0
	github.com/jessevdk/go-flags v1.4.1-0.20200711081900-c17162fe8fd7
	github.com/jrick/logrotate v1.0.0
	google.golang.org/grpc v1.36.1
)
