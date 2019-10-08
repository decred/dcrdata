module github.com/decred/dcrdata/dcrrates/rateserver

require (
	github.com/decred/dcrd/certgen v1.0.2
	github.com/decred/dcrdata/dcrrates v1.1.1
	github.com/decred/dcrdata/exchanges/v2 v2.0.2
	github.com/decred/slog v1.0.0
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	google.golang.org/grpc v1.20.0
)

replace github.com/decred/dcrdata/exchanges/v2 => ../../exchanges
