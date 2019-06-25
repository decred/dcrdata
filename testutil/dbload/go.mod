module github.com/decred/dcrdata/testutil/dbload

go 1.11

replace github.com/decred/dcrdata/testutil/dbconfig => ../dbconfig

require (
	github.com/decred/dcrdata/testutil/dbconfig v1.0.1
	github.com/lib/pq v1.1.0
)
