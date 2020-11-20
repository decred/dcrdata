module github.com/decred/dcrdata/cmd/rebuilddb2

go 1.14

replace (
	github.com/decred/dcrdata/db/dcrpg/v6 => ../../db/dcrpg/
	github.com/decred/dcrdata/exchanges/v3 => ../../exchanges/
	github.com/decred/dcrdata/gov/v4 => ../../gov/
	github.com/decred/dcrdata/v6 => ../../
)

require (
	github.com/chappjc/logrus-prefix v0.0.0-20180227015900-3a1d64819adb
	github.com/decred/dcrd/chaincfg/v3 v3.0.0
	github.com/decred/dcrd/dcrutil/v3 v3.0.0
	github.com/decred/dcrd/rpcclient/v6 v6.0.2
	github.com/decred/dcrdata/db/dcrpg/v6 v6.0.0
	github.com/decred/dcrdata/v6 v6.0.0
	github.com/decred/slog v1.1.0
	github.com/dmigwi/go-piparser/proposals v0.0.0-20191219171828-ae8cbf4067e1
	github.com/jessevdk/go-flags v1.4.1-0.20200711081900-c17162fe8fd7
	github.com/mattn/go-colorable v0.1.8 // indirect
	github.com/mgutz/ansi v0.0.0-20200706080929-d51e80ef957d // indirect
	github.com/shiena/ansicolor v0.0.0-20200904210342-c7312218db18
	github.com/sirupsen/logrus v1.2.0
	github.com/x-cray/logrus-prefixed-formatter v0.5.2 // indirect
)
