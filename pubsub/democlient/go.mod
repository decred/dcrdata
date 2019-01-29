module github.com/decred/dcrdata/pubsub/democlient

require (
	github.com/btcsuite/go-flags v0.0.0-20150116065318-6c288d648c1c
	github.com/decred/dcrdata/v4 v4.0.0-20190129203925-bb16cf817719
	golang.org/x/net v0.0.0-20181217023233-e147a9138326
	gopkg.in/AlecAivazis/survey.v1 v1.8.2
)

replace github.com/decred/dcrdata/v4 => ../..
