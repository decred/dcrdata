module github.com/decred/dcrdata/db/dbtypes

go 1.11

replace github.com/decred/dcrdata/db/dcrpg => ../../db/dcrpg

require (
	github.com/decred/dcrd/blockchain/stake v1.1.0
	github.com/decred/dcrd/chaincfg v1.4.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrec v0.0.0-20190413175304-e69a789183f3 // indirect
	github.com/decred/dcrd/dcrec/edwards v0.0.0-20190413175304-e69a789183f3 // indirect
	github.com/decred/dcrd/dcrutil v1.2.1-0.20190118223730-3a5281156b73
	github.com/decred/dcrd/txscript v1.0.3-0.20190402182842-879eebce3333
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrdata/db/dcrpg v1.0.0
	github.com/decred/dcrdata/txhelpers v1.0.2-0.20190415153927-351272ba94ab
	golang.org/x/crypto v0.0.0-20190411191339-88737f569e3a // indirect
	golang.org/x/net v0.0.0-20190415214537-1da14a5a36f2 // indirect
	golang.org/x/sys v0.0.0-20190416124237-ebb4019f01c9 // indirect
)
