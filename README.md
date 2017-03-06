# dcrdata

This is very much a work-in-progres.

What it is/does presently:

* Registers for new-block notifications (and other ntfns) from dcrd
* When signaled by the notifier, collects a bunch of block data
* Saves it into sqlite and/or a map in memory
* A basic webui facility (make your template)
* Starts a HTTP JSON API and serves up what it has
* A CLI in cmd/rebuilddb that performs a full blockchain scan that fills past
  block data into SQLite.
* A CLI in cmd/scanblocks to scan the blockchain and save data into JSON.
* Various packages, such as the API types (package dcrdataapi).

What it needs (just a few top ones):

* More database backend options, perhaps PostgreSQL and/or mongodb too
* mempool data collection and storage. Collection is already implemented, but no
storage or API endpoints.  For instance, a realtime read of the ticket fee
distribution in mempool, stake submissions included as they come in.
* Chain reorg handling.
* test cases.

## Getting Started ##

```
git clone https://github.com/dcrdata/dcrdata $GOPATH/src/github.com/dcrdata/dcrdata
glide install
go build
cp ./sample-dcrdata.conf ./dcrdata.conf
vim dcrdata.conf
./dcrdata
```

The sqlite driver uses cgo, which requires gcc to compile the C sources. On
Windows this is easily handles with MSYS2 ([download](http://www.msys2.org/) and
install MinGW-w64 gcc packages.).
