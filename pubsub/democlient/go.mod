module github.com/decred/dcrdata/pubsub/democlient

require (
	github.com/decred/dcrdata/v4 v4.0.0-20190129203925-bb16cf817719
	github.com/jessevdk/go-flags v1.4.0
	golang.org/x/crypto v0.0.0-20190313024323-a1f597ede03a // indirect
	golang.org/x/net v0.0.0-20190313082753-5c2c250b6a70
	golang.org/x/sys v0.0.0-20190312061237-fead79001313 // indirect
	gopkg.in/AlecAivazis/survey.v1 v1.8.2
)

replace github.com/decred/dcrdata/v4 => ../..
