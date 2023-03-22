# dcrdata

[![Build Status](https://github.com/decred/dcrdata/workflows/Build%20and%20Test/badge.svg)](https://github.com/decred/dcrdata/actions)
[![Latest tag](https://img.shields.io/github/tag/decred/dcrdata.svg)](https://github.com/decred/dcrdata/tags)
[![Go Report Card](https://goreportcard.com/badge/github.com/decred/dcrdata)](https://goreportcard.com/report/github.com/decred/dcrdata)
[![ISC License](https://img.shields.io/badge/license-ISC-blue.svg)](http://copyfree.org)

## Overview

dcrdata is an original [Decred](https://www.decred.org/) block explorer, with
packages and apps for data collection, presentation, and storage. The backend
and middleware are written in Go. On the front end, Webpack enables the use of
modern javascript features, as well as SCSS for styling.

- [dcrdata](#dcrdata)
  - [Overview](#overview)
  - [Release Status](#release-status)
  - [Repository Overview](#repository-overview)
  - [Requirements](#requirements)
  - [Docker Support](#docker-support)
  - [Building](#building)
    - [Preparation](#preparation)
    - [Package the Static Web Assets](#package-the-static-web-assets)
    - [Building dcrdata with Go](#building-dcrdata-with-go)
    - [Setting build version flags](#setting-build-version-flags)
    - [Runtime Resources](#runtime-resources)
  - [Updating](#updating)
  - [Upgrading Instructions](#upgrading-instructions)
    - [From v3.x or later](#from-v3x-or-later)
    - [From v2.x or earlier](#from-v2x-or-earlier)
  - [Getting Started](#getting-started)
    - [Configuring PostgreSQL (**IMPORTANT!** Seriously, read this.)](#configuring-postgresql-important-seriously-read-this)
    - [Creating the dcrdata Configuration File](#creating-the-dcrdata-configuration-file)
    - [Using Environment Variables for Configuration](#using-environment-variables-for-configuration)
    - [Indexing the Blockchain](#indexing-the-blockchain)
    - [Starting dcrdata](#starting-dcrdata)
    - [Hiding the PostgreSQL Settings Table](#hiding-the-postgresql-settings-table)
    - [Running the Web Interface During Synchronization](#running-the-web-interface-during-synchronization)
  - [System Hardware Requirements](#system-hardware-requirements)
    - [dcrdata only (PostgreSQL on other host)](#dcrdata-only-postgresql-on-other-host)
    - [dcrdata and PostgreSQL on same host](#dcrdata-and-postgresql-on-same-host)
  - [dcrdata Daemon](#dcrdata-daemon)
    - [Block Explorer](#block-explorer)
  - [APIs](#apis)
    - [Insight API](#insight-api)
    - [dcrdata API](#dcrdata-api)
      - [Endpoint List](#endpoint-list)
  - [Important Note About Mempool](#important-note-about-mempool)
  - [Command Line Utilities](#command-line-utilities)
    - [rebuilddb2](#rebuilddb2)
    - [scanblocks](#scanblocks)
  - [Front End Development](#front-end-development)
    - [CSS Guidelines](#css-guidelines)
    - [HTML](#html)
    - [Javascript](#javascript)
    - [Web Performance](#web-performance)
  - [Helper Packages](#helper-packages)
  - [Internal-use Packages](#internal-use-packages)
  - [Plans](#plans)
  - [Contributing](#contributing)
  - [License](#license)

## Release Status

Always run the Current release or on the Current stable branch. Do not use `master` in production.

|             | Series  | Branch       | Latest release tag | `dcrd` RPC server version required |
| ----------- | ------- | ------------ | ------------------ | ---------------------------------- |
| Development | 6.1     | `master`     | N/A                | ^7.0.0 (dcrd v1.7 release)         |
| Current     | 6.0     | `6.0-stable` | `release-v6.0`     | ^6.2.0 (dcrd v1.6 release)         |

## Repository Overview

```none
../dcrdata                The main Go MODULE. See cmd/dcrdata for the explorer executable.
├── api/types             The exported structures used by the dcrdata and Insight APIs.
├── blockdata             Package blockdata is the primary data collection and
|                           storage hub, and chain monitor.
├── cmd
│   └── dcrdata           MODULE for the dcrdata explorer executable.
│       ├── api           dcrdata's own HTTP API
│       │   └── insight   The Insight API
│       ├── explorer      Powers the block explorer pages.
│       ├── middleware    HTTP router middleware used by the explorer
│       ├── notification  Manages dcrd notifications synchronous data collection.
│       ├── public        Public resources for block explorer (css, js, etc.)
│       └── views         HTML templates for block explorer
├── db
│   ├── cache             Package cache provides a caching layer that is used by dcrpg.
│   ├── dbtypes           Package dbtypes with common data types.
│   └── dcrpg             MODULE and package dcrpg providing PostgreSQL backend.
├── dev                   Shell scripts for maintenance and deployment.
├── docs                  Extra documentation.
├── exchanges             MODULE and package for gathering data from public exchange APIs
│   ├── rateserver        rateserver app, which runs an exchange bot for collecting
│   |                       exchange rate data, and a gRPC server for providing this
│   |                       data to multiple clients like dcrdata.
|   └── ratesproto        Package dcrrates implementing a gRPC protobuf service for
|                           communicating exchange rate data with a rateserver.
├── explorer/types        Types used primarily by the explorer pages.
├── gov                   MODULE for the on- and off-chain governance packages.
│   ├── agendas           Package agendas defines a consensus deployment/agenda DB.
│   └── politeia          Package politeia defines a Politeia proposal DB.
│       ├── piclient      Package piclient provides functions for retrieving data
|       |                   from the Politeia web API.
│       └── types         Package types provides several JSON-tagged structs for
|                           dealing with Politeia data exchange.
├── mempool               Package mempool for monitoring mempool for transactions,
|                           data collection, distribution, and storage.
├── netparams             Package netparams defines the TCP port numbers for the
|                           various networks (mainnet, testnet, simnet).
├── pubsub                Package pubsub implements a websocket-based pub-sub server
|   |                       for blockchain data.
│   ├── democlient        democlient app provides an example for using psclient to
|   |                       register for and receive messages from a pubsub server.
│   ├── psclient          Package psclient is a basic client for the pubsub server.
│   └── types             Package types defines types used by the pubsub client
|                           and server.
├── rpcutils              Package rpcutils contains helper types and functions for
|                           interacting with a chain server via RPC.
├── semver                Defines the semantic version types.
├── stakedb               Package stakedb, for tracking tickets
├── testutil
│   ├── apiload           An HTTP API load testing application
|   └── dbload            A DB load testing application
└── txhelpers             Package txhelpers provides many functions and types for
                            processing blocks, transactions, voting, etc.
```

## Requirements

- [Go](https://golang.org) 1.19 or 1.20
- [Node.js](https://nodejs.org/en/download/) 16.x or 18.x. Node.js is only used
  as a build tool, and is **not used at runtime**.
- Running `dcrd` running with `--txindex`, and synchronized to the current best
  block on the network. On startup, dcrdata will verify that the dcrd version is
  compatible.
- PostgreSQL 11+

## Docker Support

Dockerfiles are provided for convenience, but NOT SUPPORTED. See [the Docker
documentation](docs/docker.md) for more information. The supported dcrdata build
instructions are described below.

## Building

The dcrdata build process comprises two general steps:

1. Bundle the static web page assets with Webpack (via the `npm` tool).
2. Build the `dcrdata` executable from the Go source files.

These steps are described in detail in the following sections.

NOTE: The following instructions assume a Unix-like shell (e.g. bash).

### Preparation

- [Install Go](https://golang.org/doc/install)

- Verify Go installation:

  ```sh
  go env GOROOT GOPATH
  ```

- Ensure `$GOPATH/bin` is on your `$PATH`.

- Clone the dcrdata repository. It is conventional to put it under `GOPATH`, but
  this is no longer necessary (or recommend) with Go modules. For example:

  ```sh
  git clone https://github.com/decred/dcrdata $HOME/go-work/github/decred/dcrdata
  ```

- [Install Node.js](https://nodejs.org/en/download/), which is required to lint
  and package the static web assets.

Note that none of the above is required at runtime.

### Package the Static Web Assets

[Webpack](https://webpack.js.org/), a JavaScript module bundler, is used to
compile and package the static assets in the `cmd/dcrdata/public` folder.
Node.js' `npm` tool is used to install the required Node.js dependencies and
build the bundled JavaScript distribution for deployment.

First, install the build dependencies:

```sh
cd cmd/dcrdata
npm clean-install # creates node_modules folder fresh
```

Then, for production, build the webpack bundle:

```sh
npm run build # creates public/dist folder
```

Alternatively, for development, `npm` can be made to watch for and integrate
JavaScript source changes:

```sh
npm run watch
```

See [Front End Development](#front-end-development) for more information.

### Building dcrdata with Go

Change to the `cmd/dcrdata` folder and build:

```sh
cd cmd/dcrdata
go build -v
```

The go tool will process the source code and automatically download
dependencies. If the dependencies are configured correctly, there will be no
modifications to the `go.mod` and `go.sum` files.

Note that performing the above commands with older versions of Go within
`$GOPATH` may require setting `GO111MODULE=on`.

As a reward for reading this far, you may use the [build.sh](dev/build.sh)
script to mostly automate the build steps.

### Setting build version flags

By default, the version string will be postfixed with "-pre+dev".  For example,
`dcrdata version 5.1.0-pre+dev (Go version go1.12.7)`.  However, it may be
desirable to set the "pre" and "dev" values to different strings, such as
"beta" or the actual commit hash.  To set these values, build with the
`-ldflags` switch as follows:

```sh
go build -v -ldflags \
  "-X main.appPreRelease=beta -X main.appBuild=`git rev-parse --short HEAD`"
```

This produces a string like `dcrdata version 6.0.0-beta+750fd6c2 (Go version go1.16.2)`.

### Runtime Resources

The config file, logs, and data files are stored in the application data folder,
which may be specified via the `-A/--appdata` and `-b/--datadir` settings.
However, the location of the config file may also be set with `-C/--configfile`.
The default paths for your system are shown in the `--help` description.
If encountering errors involving file system paths, check the permissions on these
folders to ensure that _the user running dcrdata_ is able to access these paths.

The "public" and "views" folders _must_ be in the same folder as the `dcrdata`
executable. Set read-only permissions as appropriate.

## Updating

Update the repository (assuming you have `master` checked out in `GOPATH`):

```sh
cd $HOME/go-work/github/decred/dcrdata
git pull origin master
```

Look carefully for errors with `git pull`, and reset locally modified files if
necessary.

Next, build `dcrdata` and bundle the web assets:

```sh
cd cmd/dcrdata
go build -v
npm clean-install
npm run build # or npm run watch
```

Note that performing the above commands with versions of Go prior to 1.16
within `$GOPATH` may require setting `GO111MODULE=on`.

## Upgrading Instructions

### From v3.x or later

No special actions are required. Simply start the new dcrdata and automatic
database schema upgrades and table data patches will begin.

### From v2.x or earlier

The database scheme change from dcrdata v2.x to v3.x does not permit an
automatic migration. The tables must be rebuilt from scratch:

1. Drop the old dcrdata database, and create a new empty dcrdata database.

   ```sql
   -- Drop the old database.
   DROP DATABASE dcrdata;

   -- Create a new database with the same "pguser" set in the dcrdata.conf.
   CREATE DATABASE dcrdata OWNER dcrdata;
   ```

2. Delete the dcrdata data folder (i.e. corresponding to the `datadir` setting).
   By default, `datadir` is in `{appdata}/data`:

   - Linux: `~/.dcrdata/data`
   - Mac: `~/Library/Application Support/Dcrdata/data`
   - Windows: `C:\Users\<your-username>\AppData\Local\Dcrdata\data` (`%localappdata%\Dcrdata\data`)

3. With dcrd synchronized to the network's best block, start dcrdata to begin
   the initial block data sync.

## Getting Started

### Configuring PostgreSQL (**IMPORTANT!** Seriously, read this.)

It is crucial that you configure your PostgreSQL server for your hardware and
the dcrdata workload.

Read [postgresql-tuning.conf](./db/dcrpg/postgresql-tuning.conf) carefully for
details on how to make the necessary changes to your system. A helpful online
tool for determining good settings for your system is called
[PGTune](https://pgtune.leopard.in.ua/). Note that when using this tool,
subtract 1.5-2GB from your system RAM so dcrdata itself will have plenty of
memory. **DO NOT** simply use this file in place of your existing
postgresql.conf. **DO NOT** simply copy and paste these settings into the
existing postgresql.conf. It is necessary to *edit the existing
postgresql.conf*, reviewing all the settings to ensure the same configuration
parameters are not set in two different places in the file (postgres will not
complain).

If you tune PostgreSQL to fully utilize remaining RAM, you are limiting
the RAM available to the dcrdata process, which will increase as request
volume increases and its cache becomes fully utilized. Allocate sufficient
memory to dcrdata for your application, and use a reverse proxy such as nginx
with cache locking features to prevent simultaneous requests to the same
resource.

On Linux, you may wish to use a unix domain socket instead of a TCP connection.
The path to the socket depends on the system, but it is commonly
`/var/run/postgresql`. Just set this path in `pghost`.

### Creating the dcrdata Configuration File

Begin with the sample configuration file. With the default `appdata` directory
for the current user on Linux:

```sh
cp sample-dcrdata.conf ~/.dcrdata/dcrdata.conf
```

Then edit dcrdata.conf with your dcrd RPC settings. See the output of `dcrdata
--help` for a list of all options and their default values.

### Indexing the Blockchain

If dcrdata has not previously been run with the PostgreSQL database backend, it
is necessary to perform a bulk import of blockchain data and generate table
indexes. _This will be done automatically by `dcrdata`_ on a fresh startup.
**Do NOT interrupt the initial sync or use the browser interface until it is
completed.**

Note that dcrdata requires that
[dcrd](https://docs.decred.org/wallets/cli/dcrd-setup/) is
running with some optional indexes enabled. By default, these indexes are not
turned on when dcrd is installed. To enable them, set the following in
dcrd.conf:

```ini
txindex=1
```

If these parameters are not set, dcrdata will be unable to retrieve transaction
details and perform address searches, and will exit with an error mentioning
these indexes.

### Starting dcrdata

Launch the dcrdata daemon and allow the databases to process new blocks.
Concurrent synchronization of both stake and PostgreSQL databases is performed,
typically requiring between 1.5 to 8 hours. See [System Hardware
Requirements](#System-Hardware-Requirements) for more information. Please reread
[Configuring PostgreSQL (**IMPORTANT!** Seriously, read
this.)](#configuring-postgresql-important-seriously-read-this) of you have
performance issues.

On subsequent launches, only blocks new to dcrdata are processed.

```sh
./dcrdata    # don't forget to configure dcrdata.conf in the appdata folder!
```

**Do NOT interrupt the initial sync or use the browser interface until it is
completed.** Follow the messages carefully, and if you are uncertain of the
current sync status, check system resource utilization. Interrupting the
initial sync can leave dcrdata and it's databases in an unrecoverable or
suboptimal state. The main steps of the initial sync process are:

1. Initial block data import
2. Indexing
3. Spending transaction relationship updates
4. Final DB analysis and indexing
5. Catch-up to network in normal sync mode
6. Populate charts historical data
7. Update Pi repo and parse proposal records (git will be running)
8. Final catch-up and UTXO cache pre-warming
9. Update project fund data and then idle

Unlike dcrdata.conf, which must be placed in the `appdata` folder or explicitly
set with `-C`, the "public" and "views" folders _must_ be in the same folder as
the `dcrdata` executable.

## System Hardware Requirements

The time required to sync varies greatly with system hardware and software
configuration. The most important factor is the storage medium on the database
machine. An SSD (preferably NVMe, not SATA) is REQUIRED. The PostgreSQL
operations are extremely disk intensive, especially during the initial
synchronization process. Both high throughput and low latencies for fast
random accesses are essential.

### dcrdata only (PostgreSQL on other host)

Without PostgreSQL, the dcrdata process can get by with:

- 1 CPU core
- 2 GB RAM
- HDD with 8GB free space

### dcrdata and PostgreSQL on same host

These specifications assume dcrdata and postgres are running on the same machine.

Minimum:

- 2 CPU core
- 6 GB RAM
- SSD with 120GB free space (no spinning hard drive for the DB!)

Recommend:

- 3+ CPU cores
- 12+ GB RAM
- NVMe SSD with 120 GB free space

## dcrdata Daemon

The `cmd/dcrdata` folder contains the `main` package for the `dcrdata` app, which
has several components including:

1. Block explorer (web interface).
2. Blockchain monitoring and data collection.
3. Mempool monitoring and reporting.
4. Database backend interfaces.
5. RESTful JSON API (custom and Insight) over HTTP(S).
6. Websocket-based pub-sub server.
7. Exchange rate bot and gRPC server.

### Block Explorer

After dcrdata syncs with the blockchain server via RPC, by default it will begin
listening for HTTP connections on `http://127.0.0.1:7777/`. This means it starts
a web server listening on IPv4 localhost, port 7777. Both the interface and port
are configurable. The block explorer and the JSON APIs are both provided by the
server on this port.

Note that while dcrdata can be started with HTTPS support, it is recommended to
employ a reverse proxy such as Nginx ("engine x"). See sample-nginx.conf for an
example Nginx configuration.

## APIs

The dcrdata block explorer is exposed by two APIs: a Decred implementation of
the [Insight API](https://github.com/bitpay/insight-api), and its
own JSON HTTP API. The Insight API uses the path prefix `/insight/api`. The
dcrdata API uses the path prefix `/api`.
File downloads are served from the `/download` path.

### Insight API

The [Insight API](https://github.com/bitpay/insight-api) is accessible via HTTP
via REST or WebSocket.

See the [Insight API documentation](docs/Insight_API_documentation.md) for
further details.

### dcrdata API

The dcrdata API is a REST API accessible via HTTP. To call the dcrdata API, use
the `/api` path prefix.

#### Endpoint List

| Best block           | Path                                 | Type                                  |
| -------------------- | ------------------------------------ | ------------------------------------- |
| Summary              | `/block/best?txtotals=[true\|false]` | `types.BlockDataBasic`                |
| Stake info           | `/block/best/pos`                    | `types.StakeInfoExtended`             |
| Header               | `/block/best/header`                 | `dcrjson.GetBlockHeaderVerboseResult` |
| Raw Header (hex)     | `/block/best/header/raw`             | `string`                              |
| Hash                 | `/block/best/hash`                   | `string`                              |
| Height               | `/block/best/height`                 | `int`                                 |
| Raw Block (hex)      | `/block/best/raw`                    | `string`                              |
| Size                 | `/block/best/size`                   | `int32`                               |
| Subsidy              | `/block/best/subsidy`                | `types.BlockSubsidies`                |
| Transactions         | `/block/best/tx`                     | `types.BlockTransactions`             |
| Transactions Count   | `/block/best/tx/count`               | `types.BlockTransactionCounts`        |
| Verbose block result | `/block/best/verbose`                | `dcrjson.GetBlockVerboseResult`       |

| Block X (block index) | Path                  | Type                                  |
| --------------------- | --------------------- | ------------------------------------- |
| Summary               | `/block/X`            | `types.BlockDataBasic`                |
| Stake info            | `/block/X/pos`        | `types.StakeInfoExtended`             |
| Header                | `/block/X/header`     | `dcrjson.GetBlockHeaderVerboseResult` |
| Raw Header (hex)      | `/block/X/header/raw` | `string`                              |
| Hash                  | `/block/X/hash`       | `string`                              |
| Raw Block (hex)       | `/block/X/raw`        | `string`                              |
| Size                  | `/block/X/size`       | `int32`                               |
| Subsidy               | `/block/best/subsidy` | `types.BlockSubsidies`                |
| Transactions          | `/block/X/tx`         | `types.BlockTransactions`             |
| Transactions Count    | `/block/X/tx/count`   | `types.BlockTransactionCounts`        |
| Verbose block result  | `/block/X/verbose`    | `dcrjson.GetBlockVerboseResult`       |

| Block H (block hash) | Path                       | Type                                  |
| -------------------- | -------------------------- | ------------------------------------- |
| Summary              | `/block/hash/H`            | `types.BlockDataBasic`                |
| Stake info           | `/block/hash/H/pos`        | `types.StakeInfoExtended`             |
| Header               | `/block/hash/H/header`     | `dcrjson.GetBlockHeaderVerboseResult` |
| Raw Header (hex)     | `/block/hash/H/header/raw` | `string`                              |
| Height               | `/block/hash/H/height`     | `int`                                 |
| Raw Block (hex)      | `/block/hash/H/raw`        | `string`                              |
| Size                 | `/block/hash/H/size`       | `int32`                               |
| Subsidy              | `/block/best/subsidy`      | `types.BlockSubsidies`                |
| Transactions         | `/block/hash/H/tx`         | `types.BlockTransactions`             |
| Transactions count   | `/block/hash/H/tx/count`   | `types.BlockTransactionCounts`        |
| Verbose block result | `/block/hash/H/verbose`    | `dcrjson.GetBlockVerboseResult`       |

| Block range (X < Y)                     | Path                      | Type                     |
| --------------------------------------- | ------------------------- | ------------------------ |
| Summary array for blocks on `[X,Y]`     | `/block/range/X/Y`        | `[]types.BlockDataBasic` |
| Summary array with block index step `S` | `/block/range/X/Y/S`      | `[]types.BlockDataBasic` |
| Size (bytes) array                      | `/block/range/X/Y/size`   | `[]int32`                |
| Size array with step `S`                | `/block/range/X/Y/S/size` | `[]int32`                |

| Transaction T (transaction id)       | Path                         | Type               |
| ------------------------------------ | ---------------------------- | ------------------ |
| Transaction details                  | `/tx/T?spends=[true\|false]` | `types.Tx`         |
| Transaction details w/o block info   | `/tx/trimmed/T`              | `types.TrimmedTx`  |
| Inputs                               | `/tx/T/in`                   | `[]types.TxIn`     |
| Details for input at index `X`       | `/tx/T/in/X`                 | `types.TxIn`       |
| Outputs                              | `/tx/T/out`                  | `[]types.TxOut`    |
| Details for output at index `X`      | `/tx/T/out/X`                | `types.TxOut`      |
| Vote info (ssgen transactions only)  | `/tx/T/vinfo`                | `types.VoteInfo`   |
| Ticket info (sstx transactions only) | `/tx/T/tinfo`                | `types.TicketInfo` |
| Serialized bytes of the transaction  | `/tx/hex/T`                  | `string`           |
| Same as `/tx/trimmed/T`              | `/tx/decoded/T`              | `types.TrimmedTx`  |

| Transactions (batch)                                    | Path                        | Type                |
| ------------------------------------------------------- | --------------------------- | ------------------- |
| Transaction details (POST body is JSON of `types.Txns`) | `/txs?spends=[true\|false]` | `[]types.Tx`        |
| Transaction details w/o block info                      | `/txs/trimmed`              | `[]types.TrimmedTx` |

| Address A                                                               | Path                            | Type                  |
| ----------------------------------------------------------------------- | ------------------------------- | --------------------- |
| Summary of last 10 transactions                                         | `/address/A`                    | `types.Address`       |
| Number and value of spent and unspent outputs                           | `/address/A/totals`             | `types.AddressTotals` |
| Verbose transaction result for last <br> 10 transactions                | `/address/A/raw`                | `types.AddressTxRaw`  |
| Summary of last `N` transactions                                        | `/address/A/count/N`            | `types.Address`       |
| Verbose transaction result for last <br> `N` transactions               | `/address/A/count/N/raw`        | `types.AddressTxRaw`  |
| Summary of last `N` transactions, skipping `M`                          | `/address/A/count/N/skip/M`     | `types.Address`       |
| Verbose transaction result for last <br> `N` transactions, skipping `M` | `/address/A/count/N/skip/M/raw` | `types.AddressTxRaw`  |
| Transaction inputs and outputs as a CSV formatted file.                 | `/download/address/io/A`        | CSV file              |

| Stake Difficulty (Ticket Price)        | Path                    | Type                               |
| -------------------------------------- | ----------------------- | ---------------------------------- |
| Current sdiff and estimates            | `/stake/diff`           | `types.StakeDiff`                  |
| Sdiff for block `X`                    | `/stake/diff/b/X`       | `[]float64`                        |
| Sdiff for block range `[X,Y] (X <= Y)` | `/stake/diff/r/X/Y`     | `[]float64`                        |
| Current sdiff separately               | `/stake/diff/current`   | `dcrjson.GetStakeDifficultyResult` |
| Estimates separately                   | `/stake/diff/estimates` | `dcrjson.EstimateStakeDiffResult`  |

| Ticket Pool                                                                                    | Path                                                  | Type                        |
| ---------------------------------------------------------------------------------------------- | ----------------------------------------------------- | --------------------------- |
| Current pool info (size, total value, and average price)                                       | `/stake/pool`                                         | `types.TicketPoolInfo`      |
| Current ticket pool, in a JSON object with a `"tickets"` key holding an array of ticket hashes | `/stake/pool/full`                                    | `[]string`                  |
| Pool info for block `X`                                                                        | `/stake/pool/b/X`                                     | `types.TicketPoolInfo`      |
| Full ticket pool at block height _or_ hash `H`                                                 | `/stake/pool/b/H/full`                                | `[]string`                  |
| Pool info for block range `[X,Y] (X <= Y)`                                                     | `/stake/pool/r/X/Y?arrays=[true\|false]`<sup>\*</sup> | `[]apitypes.TicketPoolInfo` |

The full ticket pool endpoints accept the URL query `?sort=[true|false]` for
requesting the tickets array in lexicographical order. If a sorted list or list
with deterministic order is _not_ required, using `sort=false` will reduce
server load and latency. However, be aware that the ticket order will be random,
and will change each time the tickets are requested.

<sup>\*</sup>For the pool info block range endpoint that accepts the `arrays`
url query, a value of `true` will put all pool values and pool sizes into
separate arrays, rather than having a single array of pool info JSON objects.
This may make parsing more efficient for the client.

| Votes and Agendas Info            | Path                  | Type                        |
| --------------------------------- | --------------------- | --------------------------- |
| The current agenda and its status | `/stake/vote/info`    | `dcrjson.GetVoteInfoResult` |
| All agendas high level details    | `/agendas`            | `[]types.AgendasInfo`       |
| Details for agenda {agendaid}     | `/agendas/{agendaid}` | `types.AgendaAPIResponse`   |

| Mempool                                           | Path                      | Type                            |
| ------------------------------------------------- | ------------------------- | ------------------------------- |
| Ticket fee rate summary                           | `/mempool/sstx`           | `apitypes.MempoolTicketFeeInfo` |
| Ticket fee rate list (all)                        | `/mempool/sstx/fees`      | `apitypes.MempoolTicketFees`    |
| Ticket fee rate list (N highest)                  | `/mempool/sstx/fees/N`    | `apitypes.MempoolTicketFees`    |
| Detailed ticket list (fee, hash, size, age, etc.) | `/mempool/sstx/details`   | `apitypes.MempoolTicketDetails` |
| Detailed ticket list (N highest fee rates)        | `/mempool/sstx/details/N` | `apitypes.MempoolTicketDetails` |

| Exchanges                         | Path                | Type                         |
| ----------------------------------| --------------------| ---------------------------- |
| Exchange data summary             | `/exchanges`        | `exchanges.ExchangeBotState` |
| List of available currency codes  | `/exchanges/codes`  | `[]string`                   |

Exchange monitoring is off by default. Server must be started with
`--exchange-monitor` to enable exchange data.
The server will set a default currency code. To use a different code, pass URL
parameter `?code=[code]`. For example, `/exchanges?code=EUR`.

| Other                           | Path                                          | Type                                    |
| ------------------------------- | --------------------------------------------- | --------------------------------------- |
| Status                          | `/status`                                     | `types.Status`                          |
| Health (HTTP 200 or 503)        | `/status/happy`                               | `types.Happy`                           |
| Coin Supply                     | `/supply`                                     | `types.CoinSupply`                      |
| Coin Supply Circulating (Mined) | `/supply/circulating?dcr=[true\|false]`       | `int` (default) or `float` (`dcr=true`) |
| Endpoint list (always indented) | `/list`                                       | `[]string`                              |

All JSON endpoints accept the URL query `indent=[true|false]`. For example,
`/stake/diff?indent=true`. By default, indentation is off. The characters to use
for indentation may be specified with the `indentjson` string configuration
option.

## Important Note About Mempool

Although there is mempool data collection and serving, it is **very important**
to keep in mind that the mempool in your node (dcrd) is not likely to be exactly
the same as other nodes' mempool. Also, your mempool is cleared out when you
shutdown dcrd. So, if you have recently (e.g. after the start of the current
ticket price window) started dcrd, your mempool _will_ be missing transactions
that other nodes have.

## Front End Development

Make sure you have a recent version of [node and npm](https://nodejs.org/en/download/)
installed.

From the cmd/dcrdata directory, run the following command to install the node
modules.

`npm clean-install`

This will create and install into a directory named `node_modules`.

You'll also want to run `npm clean-install` after merging changes from upstream.
It is run for you when you use the build script (`./dev/build.sh`).

For development, there's a webpack script that watches for file changes and
automatically bundles. To use it, run the following command in a separate
terminal and leave it running while you work. You'll only use this command if
you are editing javascript files.

`npm run watch`

For production, bundle assets via:

`npm run build`

You will need to at least `build` if changes have been made. `watch` essentially
runs `build` after file changes, but also performs some additional checks.

### CSS Guidelines

Webpack compiles SCSS to CSS while bundling. The `watch` script described above
also watches for changes in these files and performs linting to ensure [syntax
compliance](https://github.com/stylelint/stylelint-config-standard).

Before you write any CSS, see if you can achieve your goal by using existing
classes available in Bootstrap 4. This helps prevent our stylesheets from
getting bloated and makes it easier for things to work well across a wide range
browsers & devices. Please take the time to [Read the
docs](https://getbootstrap.com/docs/4.1/getting-started/introduction/)

Note there is a dark mode, so make sure things look good with the dark
background as well.

### HTML

The core functionality of dcrdata is server-side rendered in Go and designed to
work well with javascript disabled. For users with javascript enabled,
[Turbolinks](https://github.com/turbolinks/turbolinks) creates a persistent
single page application that handles all HTML rendering.

.tmpl files are cached by the backend, and can be reloaded via running
`killall -USR1 dcrdata` from the command line.

### Javascript

To encourage code that is idiomatic to Turbolinks based execution environment,
javascript based enhancements should use [Stimulus](https://stimulusjs.org/)
controllers with corresponding actions and targets. Keeping things tightly
scoped with controllers and modules helps to localize complexity and maintain a
clean application lifecycle. When using events handlers, bind and **unbind**
them in the `connect` and `disconnect` function of controllers which executes
when they get removed from the DOM.

### Web Performance

The core functionality of dcrdata should perform well in low power device / high
latency scenarios (eg. a cheap smart phone with poor reception). This means that
heavy assets should be lazy loaded when they are actually needed. Simple tasks
like checking a transaction or address should have a very fast initial page
load.

## Helper Packages

`package dbtypes` defines the data types used by the DB backends to model the
block, transaction, and related blockchain data structures. Functions for
converting from standard Decred data types (e.g. `wire.MsgBlock`) are also
provided.

`package rpcutils` includes helper functions for interacting with a
`rpcclient.Client`.

`package stakedb` defines the `StakeDatabase` and `ChainMonitor` types for
efficiently tracking live tickets, with the primary purpose of computing ticket
pool value quickly. It uses the `database.DB` type from
`github.com/decred/dcrd/database` with an ffldb storage backend from
`github.com/decred/dcrd/database/ffldb`. It also makes use of the `stake.Node`
type from `github.com/decred/dcrd/blockchain/stake`. The `ChainMonitor` type
handles connecting new blocks and chain reorganization in response to notifications
from dcrd.

`package txhelpers` includes helper functions for working with the common types
`dcrutil.Tx`, `dcrutil.Block`, `chainhash.Hash`, and others.

## Internal-use Packages

Some packages are currently designed only for internal
use by other dcrdata packages, but may be of general value in
the future.

`blockdata` defines:

- The `chainMonitor` type and its `BlockConnectedHandler()` method that handles
  block-connected notifications and triggers data collection and storage.
- The `BlockData` type and methods for converting to API types.
- The `blockDataCollector` type and its `Collect()` and `CollectHash()` methods
  that are called by the chain monitor when a new block is detected.
- The `BlockDataSaver` interface required by `chainMonitor` for storage of
  collected data.

`dcrpg` defines:

- The `ChainDB` type, which is the primary exported type from `dcrpg`, providing
  an interface for a PostgreSQL database.
- A large set of lower-level functions to perform a range of queries given a
  `*sql.DB` instance and various parameters.
- The internal package contains the raw SQL statements.

`package mempool` defines a `MempoolMonitor` type that can monitor a node's
mempool using the `OnTxAccepted` notification handler to send newly received
transaction hashes via a designated channel. Ticket purchases (SSTx) are
triggers for mempool data collection, which is handled by the
`DataCollector` class, and data storage, which is handled by any number
of objects implementing the `MempoolDataSaver` interface.

## Plans

See the GitHub [issue trackers](https://github.com/decred/dcrdata/issues) and
the [project milestones](https://github.com/decred/dcrdata/milestones).

## Contributing

Yes, please! **See [CONTRIBUTING.md](docs/CONTRIBUTING.md) for details**, but
here's the gist of it:

1. Fork the repo.
2. Create a branch for your work (`git checkout -b cool-stuff`).
3. Code something great.
4. Commit and push to your repo.
5. Create a [pull request](https://github.com/decred/dcrdata/compare).

**DO NOT merge from master to your feature branch; rebase.**

Also, [come chat with us on Matrix](https://chat.decred.org/) in the
dcrdata channel!

## License

This project is licensed under the ISC License. See the [LICENSE](LICENSE) file
for details.
