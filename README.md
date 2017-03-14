# dcrdata

[![Build Status](http://img.shields.io/travis/dcrdata/dcrdata.svg)](https://travis-ci.org/dcrdata/dcrdata)
[![GitHub release](https://img.shields.io/github/release/dcrdata/dcrdata.svg)](https://github.com/dcrdata/dcrdata/releases)
[![Latest tag](https://img.shields.io/github/tag/dcrdata/dcrdata.svg)](https://github.com/dcrdata/dcrdata/tags)
[![ISC License](http://img.shields.io/badge/license-ISC-blue.svg)](http://copyfree.org)

The dcrdata repository is a collection of golang packages and apps for Decred data
collection, storage, and presentation.

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
├── rpcutils            Package rpcutils.
├── semver              Package semver.
├── txhelpers           Package txhelpers.
└── views               HTML temlates for web UI.
```

## dcrdata daemon

The root of the repository is the `main` package for the dcrdata app, which has
several components including:

1. Block chain monitoring and data collection.
1. Data storage in durable database.
1. RESTful JSON API over HTTP(S).
1. Web interface.

### REST API

The API serves JSON data over HTTP(S).  After dcrdata syncs with the blockchain
server, by default it will begin listening on `http://0.0.0.0:7777/`.  This means
it starts a web server listening on all network interfaces on port 7777. All API
endpoints are prefixed with `/api`.

Some example endpoints:

| Best block | |
|----------|---------------|
| Summary | /api/block/best |
| Stake info |  /api/block/best/pos |
| Header |  /api/block/best/header |


| Block X | |
|----------|---------------|
| Summary | /api/block/6666 |
| Stake info |  /api/block/6666/pos |
| Header |  /api/block/6666/header |

| Block range | |
|----------|---------------|
| Summary array | /api/block/range/6664/6666 |

| Stake Difficulty | |
|--------|-----------|
| Current sdiff and estimates | /api/stake |
| Current sdiff separately | /api/stake/current |
| Estimates separately | /api/stake/estimates |

| Other | |
|--------|-----------|
| Status | /status |
| Directory | /directory |

### Web Interface

In addition to the API that is accessible via paths beginning with `/api`, an
HTML interface is served on the root path (`/`).

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
`dcrrpcclient.Client`.

`package txhelpers` includes helper function for working with the common types
`dcrutil.Tx`, `dcrutil.Block`, `chainhash.Hash`, and others.

## Internal-use packages

Packages `blockdata` and `dcrsqlite` are currenly designed only for internal use
by other dcrdata packages, but they may be of general value in the future.

`blockdata` defines:

* The `chainMonitor` type and its `BlockConnectedHandler()` method that handles
  block-connected notifications and triggers data collection and storage.
* The `BlockData` type and methods for converting to API types.
* The `blockDataCollector` type its `Collect()` method that is called by
  the chain monitor when a new block is detected.
* The `BlockDataSaver` interface required by `chainMonitor` for storage of
  collected data.

`dcrsqlite` defines:

* A `sql.DB` wrapper type (`DB`) with the necessary SQLite queries for
  storage and retrieval of block and stake data.
* The `wiredDB` type, intended to satisfy the `APIDataSource` interface used by
  the dcrdata app's API. The block header is not stored in the DB, so a RPC
  client is used by `wiredDB` to get it on demand. `wiredDB` also includes
  methods to resync the database file.

## Plans

The GitHub issue tracker for dcrdata lists planned improvements. A few important
ones:

* More database backend options, perhaps PostgreSQL and/or mongodb.
* mempool data collection and storage. Collection is already implemented, but no
  storage or API endpoints.  For instance, a realtime read of the ticket fee
  distribution in mempool, stake submissions included as they come in.
* Chain reorg handling.
* test cases.

## Requirements

* [Go](http://golang.org) 1.7 or newer.
* Running `dcrd` (>=0.6.1) synchronized to the current best block on the network.

## Installation

### Build from Source

The following instructions assume a Unix-like shell (e.g. bash).

* [Install Go](http://golang.org/doc/install)

* Verify Go installation:

        go env GOROOT GOPATH

* Ensure $GOPATH/bin is on your $PATH
* Install glide

        go get -u -v github.com/Masterminds/glide

* Clone dcrdata repo

        git clone https://github.com/dcrdata/dcrdata $GOPATH/src/github.com/dcrdata/dcrdata

* Glide install, and build executable

        cd $GOPATH/src/github.com/dcrdata/dcrdata
        glide install
        go build # or go install $(glide nv)

The sqlite driver uses cgo, which requires gcc to compile the C sources. On
Windows this is easily handles with MSYS2 ([download](http://www.msys2.org/) and
install MinGW-w64 gcc packages).

If you receive other build errors, it may be due to "vendor" directories left by
glide builds of dependencies such as dcrwallet.  You may safely delete vendor
folders.

## Updating

First, update the repository (assuming you have `master` checked out):

    cd $GOPATH/src/github.com/dcrdata/dcrdata
    git pull origin master

Look carefully for errors with git pull, and revert changed files if necessary.
Then follow the install instructions starting at "Glide install...".

## Getting Started

Create configuration file.

```bash
cp ./sample-dcrdata.conf ./dcrdata.conf
```

Then edit dcrdata.conf with your dcrd RPC settings.

Finally, launch the daemon and allow the databases to sync.

```bash
./dcrdata
```
