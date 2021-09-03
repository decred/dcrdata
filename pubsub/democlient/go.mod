module github.com/decred/dcrdata/pubsub/democlient

go 1.16

replace github.com/decred/dcrdata/v7 => ../../

require (
	github.com/decred/dcrd/chaincfg/v3 v3.0.1-0.20210525214639-70483c835b7f
	github.com/decred/dcrd/txscript/v4 v4.0.0-20210901152745-8830d9c9cdba
	github.com/decred/dcrdata/v7 v7.0.0
	github.com/decred/slog v1.2.0
	github.com/jessevdk/go-flags v1.4.1-0.20200711081900-c17162fe8fd7
	github.com/kr/pty v1.1.4 // indirect
	gopkg.in/AlecAivazis/survey.v1 v1.8.2
)
