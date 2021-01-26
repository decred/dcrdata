module github.com/decred/dcrdata/pubsub/democlient

go 1.14

replace github.com/decred/dcrdata/v6 => ../../

require (
	github.com/decred/dcrd/chaincfg/v3 v3.0.0
	github.com/decred/dcrd/dcrutil/v3 v3.0.0
	github.com/decred/dcrdata/v6 v6.0.0
	github.com/decred/slog v1.1.0
	github.com/jessevdk/go-flags v1.4.1-0.20200711081900-c17162fe8fd7
	github.com/kr/pty v1.1.4 // indirect
	gopkg.in/AlecAivazis/survey.v1 v1.8.2
)
