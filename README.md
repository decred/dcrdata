# dcrdata

[![Build Status](https://img.shields.io/travis/dcrdata/dcrdata.svg)](https://travis-ci.org/dcrdata/dcrdata)
[![GitHub release](https://img.shields.io/github/release/dcrdata/dcrdata.svg)](https://github.com/dcrdata/dcrdata/releases)
[![Latest tag](https://img.shields.io/github/tag/dcrdata/dcrdata.svg)](https://github.com/dcrdata/dcrdata/tags)
[![ISC License](https://img.shields.io/badge/license-ISC-blue.svg)](http://copyfree.org)

The dcrdata repository is a collection of golang packages and apps for [Decred](https://www.decred.org/) data collection, storage, and presentation.

## Repository overview

```none
../dcrdata              The dcrdata daemon.
├── blockdata           Package blockdata.
├── cmd
│   ├── rebuilddb       rebuilddb utility.
│   └── scanblocks      scanblocks utility.
├── dcrdataapi          Package dcrdataapi for golang API clients.
├── dcrsqlite           Package dcrsqlite providing SQLite backend.
├── public              Public resources for web UI (css, js, etc.).
├── mempool             Package mempool.
├── rpcutils            Package rpcutils.
├── semver              Package semver.
├── stakedb             Package stakedb, for tracking tickets.
├── txhelpers           Package txhelpers.
└── views               HTML templates for web UI.
```

## dcrdata daemon

The root of the repository is the `main` package for the dcrdata app, which has
several components including:

1. Blockchain monitoring and data collection.
1. Mempool monitoring and reporting.
1. Data storage in durable database (sqlite presently).
1. RESTful JSON API over HTTP(S).
1. Basic web interface.

### JSON REST API

The API serves JSON data over HTTP(S).  After dcrdata syncs with the blockchain
server, by default it will begin listening on `http://0.0.0.0:7777/`.  This
means it starts a web server listening on all network interfaces on port 7777.
**All API endpoints are currently prefixed with `/api`** (e.g.
`http://localhost:7777/api/stake`), but this may be configurable in the future.

#### Endpoint List

| Best block | |
| --- | --- |
| Summary | `/block/best` |
| Stake info |  `/block/best/pos` |
| Header |  `/block/best/header` |
| Hash |  `/block/best/hash` |
| Height | `/block/best/height` |
| Size | `/block/best/size` |
| Transactions | `/block/best/tx` |
| Transactions Count | `/block/best/tx/count` |
| Verbose block result | `/block/best/verbose` |


| Block X (block index) | |
| --- | --- |
| Summary | `/block/X` |
| Stake info |  `/block/X/pos` |
| Header |  `/block/X/header` |
| Hash |  `/block/X/hash` |
| Size | `/block/X/size` |
| Transactions | `/block/X/tx` |
| Transactions Count | `/block/X/tx/count` |
| Verbose block result | `/block/X/verbose` |

| Block H (block hash) | |
| --- | --- |
| Summary | `/block/hash/H` |
| Stake info |  `/block/hash/H/pos` |
| Header |  `/block/hash/H/header` |
| Height |  `/block/hash/H/height` |
| Size | `/block/hash/H/size` |
| Transactions | `/block/hash/H/tx` |
| Transactions Count | `/block/hash/H/tx/count` |
| Verbose block result | `/block/hash/H/verbose` |

| Block range (X < Y) | |
| --- | --- |
| Summary array for blocks on `[X,Y]` | `/block/range/X/Y` |
| Summary array with block index step `S` | `/block/range/X/Y/S` |
| Size (bytes) array | `/block/range/X/Y/size` |
| Size array with step `S` | `/block/range/X/Y/S/size` |

| Transaction T (transaction id) | |
| --- | --- |
| Transaction Details | `/tx/T` |
| Inputs | `/tx/T/in` |
| Details for input at index `X` | `/tx/T/in/X` |
| Outputs | `/tx/T/out` |
| Details for output at index `X` | `/tx/T/out/X` |

| Address A | |
| --- | --- |
| Summary of last 10 transactions | `/address/A` |
| Verbose transaction result for last <br> 10 transactions | `/address/A/raw` |
| Summary of last `N` transactions | `/address/A/count/N` |
| Verbose transaction result for last <br> `N` transactions | `/address/A/count/N/raw` |

| Stake Difficulty (Ticket Price) | |
| --- | --- |
| Current sdiff and estimates | `/stake/diff` |
| Sdiff for block `X` | `/stake/diff/b/X` |
| Sdiff for block range `[X,Y] (X <= Y)` | `/stake/diff/r/X/Y` |
| Current sdiff separately | `/stake/diff/current` |
| Estimates separately | `/stake/diff/estimates` |

| Ticket Pool | |
| --- | --- |
| Current pool info (size, total value, and average price) | `/stake/pool` |
| Pool info for block `X` | `/stake/pool/b/X` |
| Pool info for block range `[X,Y] (X <= Y)` | `/stake/pool/r/X/Y?arrays=[true\|false]` <sup>*</sup> |

<sup>*</sup>For the pool info block range endpoint that accepts the `arrays` url query,
a value of `true` will put all pool values and pool sizes into separate arrays,
rather than having a single array of pool info JSON objects.  This may make
parsing more efficient for the client.

| Mempool | |
| --- | --- |
| Ticket fee rate summary | `/mempool/sstx` |
| Ticket fee rate list (all) | `/mempool/sstx/fees` |
| Ticket fee rate list (N highest) | `/mempool/sstx/fees/N` |
| Detailed ticket list (fee, hash, size, age, etc.) | `/mempool/sstx/details` 
| Detailed ticket list (N highest fee rates) | `/mempool/sstx/details/N`|

| Other | |
| --- | --- |
| Status | `/status` |
| Endpoint list (always indented) | `/list` |
| Directory | `/directory` |

All JSON endpoints accept the URL query `indent=[true|false]`.  For example,
`/stake/diff?indent=true`. By default, indentation is off. The characters to use
for indentation may be specified with the `indentjson` string configuration
option.

### Web Interface

In addition to the API that is accessible via paths beginning with `/api`, an
HTML interface is served on the root path (`/`).

## Important Note About Mempool

Although there is mempool data collection and serving, it is **very important**
to keep in mind that the mempool in your node (dcrd) is not likely to be the
same as other nodes' mempool.  Also, your mempool is cleared out when you
shutdown dcrd.  So, if you have recently (e.g. after the start of the current
ticket price window) started dcrd, your mempool _will_ be missing transactions
that other nodes have.

## Command Line Utilities

### rebuilddb

rebuilddb is a CLI app that performs a full blockchain scan that fills past
block data into a SQLite database. This functionality is included in the startup
of the dcrdata daemon, but may be called alone with rebuilddb.

### scanblocks

scanblocks is a CLI app to scan the blockchain and save data into a JSON file.
More details are in [its own README](./cmd/scanblocks/README.md). The repository
also includes a shell script, jsonarray2csv.sh, to convert the result into a
comma-separated value (CSV) file.

## Helper packages

`package dcrdataapi` defines the data types, with json tags, used by the JSON
API.  This facilitates authoring of robust golang clients of the API.

`package rpcutils` includes helper functions for interacting with a
`rpcclient.Client`.

`package stakedb` defines the `StakeDatabase` and `ChainMonitor` types for
efficiently tracking live tickets, with the primary purpose of computing ticket
pool value quickly.  It uses the `database.DB` type from
`github.com/decred/dcrd/database` with an ffldb storage backend from
`github.com/decred/dcrd/database/ffldb`.  It also makes use of the `stake.Node`
type from `github.com/decred/dcrd/blockchain/stake`.  The `ChainMonitor` type
handles connecting new blocks and chain reorganiation in response to notifications
from dcrd.

`package txhelpers` includes helper functions for working with the common types
`dcrutil.Tx`, `dcrutil.Block`, `chainhash.Hash`, and others.

## Internal-use packages

Packages `blockdata` and `dcrsqlite` are currently designed only for internal use
by other dcrdata packages, but they may be of general value in the future.

`blockdata` defines:

* The `chainMonitor` type and its `BlockConnectedHandler()` method that handles
  block-connected notifications and triggers data collection and storage.
* The `BlockData` type and methods for converting to API types.
* The `blockDataCollector` type and its `Collect()` and `CollectHash()` methods
  that are called by the chain monitor when a new block is detected.
* The `BlockDataSaver` interface required by `chainMonitor` for storage of
  collected data.

`dcrsqlite` defines:

* A `sql.DB` wrapper type (`DB`) with the necessary SQLite queries for
  storage and retrieval of block and stake data.
* The `wiredDB` type, intended to satisfy the `APIDataSource` interface used by
  the dcrdata app's API. The block header is not stored in the DB, so a RPC
  client is used by `wiredDB` to get it on demand. `wiredDB` also includes
  methods to resync the database file.

`package mempool` defines a `mempoolMonitor` type that can monitor a node's
mempool using the `OnTxAccepted` notification handler to send newly received
transaction hashes via a designated channel. Ticket purchases (SSTx) are
triggers for mempool data collection, which is handled by the
`mempoolDataCollector` class, and data storage, which is handled by any number
of objects implementing the `MempoolDataSaver` interface.

## Plans

See the GitHub issue tracker and the [project milestones](https://github.com/dcrdata/dcrdata/milestones).

## Requirements

* [Go](http://golang.org) 1.8.3 or newer.
* Running `dcrd` (>=0.6.1) synchronized to the current best block on the network.

## Installation

### Build from Source

The following instructions assume a Unix-like shell (e.g. bash).

* [Install Go](http://golang.org/doc/install)

* Verify Go installation:

      go env GOROOT GOPATH

* Ensure `$GOPATH/bin` is on your `$PATH`
* Install `dep`, the dependency management tool

      go get -u -v github.com/golang/dep/cmd/dep

* Clone the dcrdata repository

      git clone https://github.com/dcrdata/dcrdata $GOPATH/src/github.com/dcrdata/dcrdata

* dep ensure, and build executable

      cd $GOPATH/src/github.com/dcrdata/dcrdata
      dep ensure
      # build dcrdata executable in workspace:
      go build
      # or to install dcrdata and other tools into $GOPATH/bin:
      go install . ./cmd/...

The sqlite driver uses cgo, which requires gcc to compile the C sources. On
Windows this is easily handled with MSYS2 ([download](http://www.msys2.org/) and
install MinGW-w64 gcc packages).

If you receive other build errors, it may be due to "vendor" directories left by
dep builds of dependencies such as dcrwallet. You may safely delete vendor
folders and run `dep ensure` again.

## Updating

First, update the repository (assuming you have `master` checked out):

    cd $GOPATH/src/github.com/dcrdata/dcrdata
    git pull origin master
    dep ensure
    go build

Look carefully for errors with `git pull`, and reset locally modified files if necessary.

## Getting Started

Create configuration file.

```bash
cp ./sample-dcrdata.conf ./dcrdata.conf
```

Then edit dcrdata.conf with your dcrd RPC settings.

Finally, launch the daemon and allow the databases to sync.  This takes about an hour on the first time. On subsequent launches, only new blocks need to be scanned.

```bash
./dcrdata
```

## Contributing

Yes, please! See the CONTRIBUTING.md file for details, but here's the gist of it:

1. Fork the repo.
1. Create a branch for your work (`git branch -b cool-stuff`).
1. Code something great.
1. Commit and push to your repo.
1. Create a [pull request](https://github.com/dcrdata/dcrdata/compare).

Note that all dcrdata.org community and team members are expected to adhere to the code of conduct, described in the CODE_OF_CONDUCT file.

## License

This project is licensed under the ISC License. See the [LICENSE](LICENSE) file for details.
