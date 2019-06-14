module github.com/decred/dcrdata/gov

go 1.11

require (
	github.com/DataDog/zstd v1.3.8 // indirect
	github.com/Sereal/Sereal v0.0.0-20190416075407-a9d24ede505a // indirect
	github.com/asdine/storm v2.2.0+incompatible
	github.com/decred/dcrd/chaincfg v1.5.1
	github.com/decred/dcrd/dcrjson/v2 v2.0.0
	github.com/decred/dcrdata/db/dbtypes/v2 v2.0.0
	github.com/decred/dcrdata/semver v1.0.0
	github.com/decred/politeia v0.0.0-20190415135723-1560639b5dd7
	github.com/decred/slog v1.0.0
	github.com/golang/protobuf v1.3.1 // indirect
	github.com/golang/snappy v0.0.1 // indirect
	github.com/pkg/errors v0.8.1 // indirect
	github.com/vmihailenco/msgpack v4.0.4+incompatible // indirect
	google.golang.org/appengine v1.5.0 // indirect
)

replace github.com/golang/lint => golang.org/x/lint v0.0.0-20190301231843-5614ed5bae6f

replace sourcegraph.com/sourcegraph/go-diff => github.com/sourcegraph/go-diff v0.5.1
