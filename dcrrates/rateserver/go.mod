module github.com/decred/dcrdata/dcrrates/rateserver

go 1.12

replace github.com/decred/dcrdata/exchanges/v2 => ../../exchanges

require (
	github.com/decred/dcrd/certgen v1.1.0
	github.com/decred/dcrdata/dcrrates v1.2.0
	github.com/decred/dcrdata/exchanges/v2 v2.1.0
	github.com/decred/slog v1.1.0
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	google.golang.org/grpc v1.24.0
)
