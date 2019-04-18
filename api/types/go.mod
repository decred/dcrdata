module github.com/decred/dcrdata/api/types/v2

go 1.11

replace github.com/decred/dcrdata/db/dcrpg => ../../db/dcrpg

require (
	github.com/AndreasBriese/bbloom v0.0.0-20190306092124-e2d15f34fcf9 // indirect
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrjson/v2 v2.0.0
	github.com/decred/dcrdata/db/dbtypes v1.1.0
	github.com/decred/dcrdata/txhelpers v1.1.0
	github.com/pkg/errors v0.8.1 // indirect
	google.golang.org/appengine v1.5.0 // indirect
)
