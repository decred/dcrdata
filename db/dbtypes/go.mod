module github.com/decred/dcrdata/db/dbtypes

go 1.11

require (
	github.com/decred/dcrdata/txhelpers v1.0.0
	github.com/decred/dcrdata/v4 v4.0.0-rc4
)

replace github.com/decred/dcrdata/txhelpers => ../../txhelpers

replace github.com/decred/dcrdata/v4 => ../..
