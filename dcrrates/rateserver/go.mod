module github.com/decred/dcrdata/dcrrates/rateserver

replace (
	github.com/decred/dcrdata/dcrrates => ../
	github.com/decred/dcrdata/exchanges => ../../exchanges
)

require (
	github.com/decred/dcrd/certgen v1.0.2
	github.com/decred/dcrdata/dcrrates v1.0.0
	github.com/decred/dcrdata/exchanges v1.0.0
	github.com/decred/slog v1.0.0
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	google.golang.org/grpc v1.19.1
)
