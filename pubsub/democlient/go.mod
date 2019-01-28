module github.com/decred/dcrdata/pubsub/democlient

require (
	github.com/Netflix/go-expect v0.0.0-20180928190340-9d1f4485533b // indirect
	github.com/btcsuite/go-flags v0.0.0-20150116065318-6c288d648c1c
	github.com/decred/dcrdata/v4 v4.0.0-20190128160712-4f93ed83b051
	github.com/hinshun/vt10x v0.0.0-20180809195222-d55458df857c // indirect
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51 // indirect
	golang.org/x/net v0.0.0-20181217023233-e147a9138326
	gopkg.in/AlecAivazis/survey.v1 v1.8.1
)

replace github.com/decred/dcrdata/v4 => ../..
