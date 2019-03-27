module github.com/decred/dcrdata/pubsub/democlient

require (
	github.com/decred/dcrdata/explorer/types v1.0.0
	github.com/decred/dcrdata/pubsub v1.0.0
	github.com/decred/dcrdata/pubsub/types v1.0.0
	github.com/jessevdk/go-flags v1.4.0
	golang.org/x/crypto v0.0.0-20190313024323-a1f597ede03a // indirect
	golang.org/x/net v0.0.0-20190326090315-15845e8f865b
	golang.org/x/sys v0.0.0-20190312061237-fead79001313 // indirect
	gopkg.in/AlecAivazis/survey.v1 v1.8.2
)

replace github.com/decred/dcrdata/pubsub => ../

replace github.com/decred/dcrdata/pubsub/types => ../types
