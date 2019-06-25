module github.com/decred/dcrdata/testutil/dbload

go 1.11

replace github.com/decred/dcrdata/testutil/dbconfig/v2 => ../dbconfig

require (
	github.com/decred/dcrdata/testutil/dbconfig/v2 v2.0.0
	github.com/lib/pq v1.1.0
)
