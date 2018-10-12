# dcrdata

[![Build Status](https://img.shields.io/travis/decred/dcrdata.svg)](https://travis-ci.org/decred/dcrdata)
[![Latest tag](https://img.shields.io/github/tag/decred/dcrdata.svg)](https://github.com/decred/dcrdata/tags)
[![Go Report Card](https://goreportcard.com/badge/github.com/decred/dcrdata)](https://goreportcard.com/report/github.com/decred/dcrdata)
[![ISC License](https://img.shields.io/badge/license-ISC-blue.svg)](http://copyfree.org)

The dcrdata repository is a collection of Go packages and apps for
[Decred](https://www.decred.org/) data collection, storage, and presentation.

- [dcrdata](#dcrdata)
  - [Repository Overview](#repository-overview)
  - [Requirements](#requirements)
  - [Docker Support](#docker-support)
    - [Building the Image](#building-the-image)
    - [Building dcrdata with Docker](#building-dcrdata-with-docker)
    - [Developing dcrdata Using a Container](#developing-dcrdata-using-a-container)
    - [Container Production Usage](#container-production-usage)
  - [Installation](#installation)
    - [Building with Go 1.11](#building-with-go-111)
    - [Building with Go 1.10](#building-with-go-110)
    - [Setting build version flags](#setting-build-version-flags)
    - [Runtime Resources](#runtime-resources)
  - [Updating](#updating)
  - [Upgrading Instructions](#upgrading-instructions)
  - [Getting Started](#getting-started)
    - [Configuring PostgreSQL (IMPORTANT)](#configuring-postgresql-important)
    - [Creating the Configuration File](#creating-the-configuration-file)
    - [Using Configuration Environment Variables](#using-configuration-environment-variables)
    - [Indexing the Blockchain](#indexing-the-blockchain)
    - [Starting dcrdata](#starting-dcrdata)
    - [Running the Web Interface During Synchronization](#running-the-web-interface-during-synchronization)
  - [System Hardware Requirements](#system-hardware-requirements)
    - ["lite" Mode (SQLite only)](#lite-mode-sqlite-only)
    - ["full" Mode (SQLite and PostgreSQL)](#full-mode-sqlite-and-postgresql)
  - [dcrdata Daemon](#dcrdata-daemon)
    - [Block Explorer](#block-explorer)
  - [APIs](#apis)
    - [Insight API (EXPERIMENTAL)](#insight-api-experimental)
    - [dcrdata API](#dcrdata-api)
      - [Endpoint List](#endpoint-list)
  - [Important Note About Mempool](#important-note-about-mempool)
  - [Command Line Utilities](#command-line-utilities)
    - [rebuilddb](#rebuilddb)
    - [rebuilddb2](#rebuilddb2)
    - [scanblocks](#scanblocks)
  - [Helper Packages](#helper-packages)
  - [Internal-use Packages](#internal-use-packages)
  - [Plans](#plans)
  - [Contributing](#contributing)
  - [License](#license)

## Repository Overview

```none
../dcrdata              The dcrdata daemon.
├── api                 Package blockdata implements dcrdata's own HTTP API.
│   ├── insight         Package insight implements the Insight API.
│   └── types           Package types includes the exported structures used by
|                         the dcrdata and Insight APIs.
├── blockdata           Package blockdata is the primary data collection and
|                         storage hub, and chain monitor.
├── cmd
│   ├── rebuilddb       rebuilddb utility, for SQLite backend. Not required.
│   ├── rebuilddb2      rebuilddb2 utility, for PostgreSQL backend. Not required.
│   └── scanblocks      scanblocks utility. Not required.
├── dcrdataapi          Package dcrdataapi for Go API clients.
├── db
│   ├── agendadb        Package agendadb is a basic PoS voting agenda database.
│   ├── dbtypes         Package dbtypes with common data types.
│   ├── dcrpg           Package dcrpg providing PostgreSQL backend.
│   └── dcrsqlite       Package dcrsqlite providing SQLite backend.
├── dev                 Shell scripts for maintenance and deployment.
├── explorer            Package explorer, powering the block explorer.
├── mempool             Package mempool for monitoring mempool for transactions,
|                         data collection, and storage.
├── middleware          Package middleware provides HTTP router middleware.
├── notification        Package notification manages dcrd notifications, and
|                         synchronous data collection by a queue of collectors.
├── public              Public resources for block explorer (css, js, etc.).
├── rpcutils            Package rpcutils contains helper types and functions for
|                         interacting with a chain server via RPC.
├── semver              Package semver.
├── stakedb             Package stakedb, for tracking tickets.
├── testutil            Package testutil provides some testing helper functions.
├── txhelpers           Package txhelpers provides many functions and types for
|                         processing blocks, transactions, voting, etc.
├── version             Package version describes the dcrdata version.
└── views               HTML templates for block explorer.
```

## Requirements

- [Go](http://golang.org) 1.10.x or 1.11.x.
- Running `dcrd` (>=1.3.0) synchronized to the current best block on the
  network. This is a strict requirement as testnet2 support is removed from
  dcrdata v3.0.0.
- (Optional) PostgreSQL 9.6+, if running in "full" mode. v10.x is recommended
  for improved dump/restore formats and utilities.

## Docker Support

The inclusion of a Dockerfile in this repository means you can use Docker for
dcrdata development or in production. However, official images are not presently
published to docker hub.

When developing you can utilize containers for easily swapping out Go versions
and overall project setup. You don't even need go installed on your system if
using containers during development.

Once [Docker](https://docs.docker.com/install/) is installed, you can then
download this repository and follow the build instructions below.

### Building the Image

To use a dcrdata container you need to build an image as follows:

`docker build --squash -t decred/dcrdata:dev-alpine .`

Note: The `--squash` flag is an [experimental
feature](https://docs.docker.com/engine/reference/commandline/image_build/) as
of Docker 18.06. Experimental features must be enabled to use the setting. On
Windows and OS/X, look under the "Daemon" settings tab. On Linux, [enable the
setting manually](https://github.com/docker/cli/blob/master/experimental/README.md).

By default, docker will build the container based on the Dockerfile found in the
root of the repository that is based on Alpine Linux. To use an Ubuntu-based
container, you should build from the Ubuntu-based Dockerfile:

`docker build --squash -f dockerfiles/Dockerfile_stretch -t decred/dcrdata:dev-stretch .`

Part of the build process is to copy all the source code over to the image,
download all dependencies, and build dcrdata. If you run into build errors with
docker try adding the `--no-cache` flag to trigger a rebuild of all the layers
since docker does not rebuild cached layers.

`docker build --no-cache --squash -t decred/dcrdata:dev-alpine .`

### Building dcrdata with Docker

In addition to running dcrdata in a container, you can also build dcrdata inside
a container and copy the executable to another system. To do this, you must have
the dcrdata Docker image or [build it from source](#building-the-image).

The default container image is based on amd64 Alpine Linux. To create a binary
targeting different operating systems or architectures, it is necessary to [set
the `GOOS` and `GOARCH` environment variables](https://golang.org/doc/install/source#environment).

From the repository source folder, do the following to build the Docker image,
and compile dcrdata into your current directory:

- `docker build --squash -t decred/dcrdata:dev-alpine .` [Only build the container image if necessary](#building-the-image)
- `docker run --entrypoint="" -v ${PWD}:/home/decred/go/src/github.com/decred/dcrdata --rm decred/dcrdata:dev-alpine go build`

This mounts your current working directory in the host machine on a volume
inside the container so that the build output will be on the host file system.

Build for other platforms as follows:

`docker run -e GOOS=darwin -e GOARCH=amd64 --entrypoint="" -v ${PWD}:/home/decred/go/src/github.com/decred/dcrdata --rm decred/dcrdata:dev-alpine go build`

`docker run -e GOOS=windows -e GOARCH=amd64 --entrypoint="" -v ${PWD}:/home/decred/go/src/github.com/decred/dcrdata --rm decred/dcrdata:dev-alpine go build`

### Developing dcrdata Using a Container

Containers are a great way to develop any source code as they serve as a
disposable runtime environment built specifically to the specifications of the
application. Suggestions for developing in a container:

1. Don't write code inside the container.
2. Attach a volume and write code from your editor on your docker host.
3. Attached volumes on a Mac are generally slower than Linux/Windows.
4. Install everything in the container, don't muck up your Docker host.
5. Resist the urge to run git commands from the container.
6. You can swap out the Go version just by using a different docker image.

To make the source code from the host available inside the container, attach a
volume to the container when launching the image:

`docker run -ti --entrypoint="" -v ${PWD}:/home/decred/go/src/github.com/decred/dcrdata --rm decred/dcrdata:dev-alpine /bin/bash`

_Note_: Changing `entrypoint` allows you to run commands in the container since
the default container command runs dcrdata. We also added /bin/bash at the
end so the container executes this by default.

You can now run `go build` or `go test` inside the container. If you run `go fmt`
you should notice that any formatting changes will also be reflected on the
docker host as well.

To run dcrdata in the container, it may be convenient to use [environment
variables](#using-configuration-environment-variables) to configure dcrdata. The
variables may be set inside the container or on the [command
line](https://docs.docker.com/engine/reference/run/#env-environment-variables).
For example,

`docker run -ti --entrypoint=/bin/bash -e DCRDATA_LISTEN_URL=0.0.0.0:2222 -v ${PWD}:/home/decred/go/src/github.com/decred/dcrdata --rm decred/dcrdata:dev-alpine`

### Container Production Usage

We don't yet have a build system in place for creating production grade images
of dcrdata. However, you can still use the images for testing.

In addition to configuring dcrdata, it is also necessary to map the TCP port on
which dcrdata listens for connections with the `-p` switch. For example,

`docker run -ti -p 2222:2222 -e DCRDATA_LISTEN_URL=0.0.0.0:2222 --rm decred/dcrdata:dev-alpine`

Please keep in mind these images have not been hardened so this is not
recommended for production.

Note: The TLS certificate for dcrd's RPC server may be needed in the container.
Either build a new container image with the certificate, or attach a volume
containing the certificate to the container.

## Installation

The following instructions assume a Unix-like shell (e.g. bash).

- [Install Go](http://golang.org/doc/install)

- Verify Go installation:

      go env GOROOT GOPATH

- Ensure `$GOPATH/bin` is on your `$PATH`.

- Clone the dcrdata repository. It is conventional to put it under `GOPATH`, but
  this is no longer necessary with go module.

      git clone https://github.com/decred/dcrdata $GOPATH/src/github.com/decred/dcrdata

- Install a C compiler. The sqlite driver uses cgo, which requires a C compiler
  (e.g. gcc) to compile the sources. On Windows this is easily handled with
  MSYS2 ([download](http://www.msys2.org/) and install MinGW-w64 gcc packages).

### Building with Go 1.11

Go 1.11 introduced [modules](https://github.com/golang/go/wiki/Modules), a new
dependency management approach, that obviates the need for third party tooling
such as `dep`.

Usage is simple and nothing is required except Go 1.11. If building in a folder
under `GOPATH`, it is necessary to explicitly build with modules enabled:

    GO111MODULE=on go build

If building outside of `GOPATH`, modules are automatically enabled, and `go
build` is sufficient.

The go tool will process the source code and automatically download
dependencies. If the dependencies are configured correctly, there will be no
modifications to the `go.mod` and `go.sum` files.

**Beware:** For the v3 dcrdata module, the executable generated by `go build`
may be named "v3" instead of "dcrdata".

### Building with Go 1.10

Module-enabled builds with Go 1.10 require the
[vgo](https://github.com/golang/vgo) command. Follow the same procedures as if
you were [using Go 1.11](#building-with-go-111), but replacing `go` with `vgo`.

**NOTE:** The `dep` tool is no longer supported. If you must use Go 1.10,
install and use `vgo`. If possible, upgrade to [Go 1.11](#building-with-go-111)
or using the Docker [container build instructions](#building-dcrdata-with-docker).

### Setting build version flags

By default, the version string will be postfixed with "-pre+dev".  For example,
`dcrdata version 3.1.0-pre+dev (Go version go1.11)`.  However, it may be
desireable to set the "pre" and "dev" values to different strings, such as
"beta" or the actual commit hash.  To set these values, build with the
`-ldflags` switch as follows:

```sh
go build -ldflags "-X github.com/decred/dcrdata/v3/version.appPreRelease=beta -X github.com/decred/dcrdata/v3/version.appBuild=`git rev-parse --short HEAD`"
```

This produces a string like `dcrdata version 3.1.0-beta+86cc62a (Go version go1.11)`.

### Runtime Resources

The config file, logs, and data files are stored in the application data folder,
which may be specified via the `-A/--appdata` and `-b/--datadir` settings.
However, the location of the config file may be set with `-C/--configfile`. If
encountering errors involving file system paths, check the permissions on these
folders to ensure that _the user running dcrdata_ is able to access these paths.

The "public" and "views" folders _must_ be in the same folder as the `dcrdata`
executable. Set read-only permissions as appropriate.

## Updating

First, update the repository (assuming you have `master` checked out):

    cd $GOPATH/src/github.com/decred/dcrdata
    git pull origin master
    go build

Look carefully for errors with `git pull`, and reset locally modified files if
necessary.

## Upgrading Instructions

_**Only necessary while upgrading from v2.x or below.**_ The database scheme
change from dcrdata v2.x to v3.x does not permit an automatic migration. The
tables must be rebuilt from scratch:

1. Drop the old dcrdata database, and create a new empty dcrdata database.

```sql
 -- drop the old database
 DROP DATABASE dcrdata;

-- create a new database with the same `pguser` set in the dcrdata.conf
CREATE DATABASE dcrdata OWNER dcrdata;

-- grant all permissions to user dcrdata
GRANT ALL PRIVILEGES ON DATABASE dcrdata to dcrdata;
```

2. Delete the dcrdata data folder (i.e. corresponding to the `datadir` setting).
   By default, `datadir` is in `{appdata}/data`:

   - Linux: `~/.dcrdata/data`
   - Mac: `~/Library/Application Support/Dcrdata/data`
   - Windows: `C:\Users\<your-username>\AppData\Local\Dcrdata\data` (`%localappdata%\Dcrdata\data`)

3. With dcrd synchronized to the network's best block, start dcrdata to begin
   the initial block data import.

## Getting Started

### Configuring PostgreSQL (IMPORTANT)

If you intend to run dcrdata in "full" mode (i.e. with the `--pg` switch), which
uses a PostgreSQL database backend, it is crucial that you configure your
PostgreSQL server for your hardware and the dcrdata workload.

Read [postgresql-tuning.conf](./db/dcrpg/postgresql-tuning.conf) carefully for
details on how to make the necessary changes to your system. A helpful online
tool for determining good settings for your system is called
[PGTune](https://pgtune.leopard.in.ua/). **DO NOT** simply use this file in place
of your existing postgresql.conf or copy and paste these settings into the
existing postgresql.conf. It is necessary to edit postgresql.conf, reviewing all
the settings to ensure the same configuration parameters are not set in two
different places in the file.

On Linux, you may wish to use a unix domain socket instead of a TCP connection.
The path to the socket depends on the system, but it is commonly
/var/run/postgresql. Just set this path in `pghost`.

### Creating the Configuration File

Begin with the sample configuration file. With the default `appdata` directory
for the current user on Linux:

```bash
cp sample-dcrdata.conf ~/.dcrdata/dcrdata.conf
```

Then edit dcrdata.conf with your dcrd RPC settings. See the output of `dcrdata --help`
for a list of all options and their default values.

### Using Configuration Environment Variables

There will be times when you don't want to fuss with a config file or cannot use
command line args such as when using Docker, Heroku, Kubernetes or other cloud
platform.

Almost all configuration items are available to set via environment variables.
To have a look at what you can set please see config.go file and the config
struct.

Each setting uses the `env` struct field tag to specify the name of the
environment variable.

ie. `env:"DCRDATA_USE_TESTNET"`

So when starting dcrdata you can now use with environment variables `DCRDATA_USE_TESTNET=true ./dcrdata`

Config precedence:

1. Command line flags have top priority
2. Config file settings
3. Environment variables
4. default config embedded in source code

Any variable that starts with USE, ENABLE, DISABLE or otherwise asks a question
must be a true/false value.

List of variables that can be set:

| Description                                                                                                              | Name                           |
| ------------------------------------------------------------------------------------------------------------------------ | ------------------------------ |
| Path to application home directory                                                                                       | DCRDATA_APPDATA_DIR            |
| Path to configuration file                                                                                               | DCRDATA_CONFIG_FILE            |
| Directory to store data                                                                                                  | DCRDATA_DATA_DIR               |
| Directory to log output                                                                                                  | DCRDATA_LOG_DIR                |
| Folder for file outputs                                                                                                  | DCRDATA_OUT_FOLDER             |
| Use the test network (default mainnet)                                                                                   | DCRDATA_USE_TESTNET            |
| Use the simulation test network (default mainnet)                                                                        | DCRDATA_USE_SIMNET             |
| Logging level {trace, debug, info, warn, error, critical}                                                                | DCRDATA_LOG_LEVEL              |
| Easy way to set debuglevel to error                                                                                      | DCRDATA_QUIET                  |
| Start HTTP profiler.                                                                                                     | DCRDATA_ENABLE_HTTP_PROFILER   |
| URL path prefix for the HTTP profiler.                                                                                   | DCRDATA_HTTP_PROFILER_PREFIX   |
| File for CPU profiling.                                                                                                  | DCRDATA_CPU_PROFILER_FILE      |
| Run with gops diagnostics agent listening. See github.com/google/gops for more information.                              | DCRDATA_USE_GOPS               |
| Protocol for API (http or https)                                                                                         | DCRDATA_ENABLE_HTTPS           |
| Listen address for API                                                                                                   | DCRDATA_LISTEN_URL             |
| Use the RealIP to get the client's real IP from the X-Forwarded-For or X-Real-IP headers, in that order.                 | DCRDATA_USE_REAL_IP            |
| Set CacheControl in the HTTP response header                                                                             | DCRDATA_MAX_CACHE_AGE          |
| Monitor mempool for new transactions, and report ticket fee info when new tickets are added.                             | DCRDATA_ENABLE_MEMPOOL_MONITOR |
| The minimum time in seconds between mempool reports, regardless of number of new tickets seen.                           | DCRDATA_MEMPOOL_MIN_INTERVAL   |
| The maximum time in seconds between mempool reports (within a couple seconds), regardless of number of new tickets seen. | DCRDATA_MEMPOOL_MAX_INTERVAL   |
| The number minimum number of new tickets that must be seen to trigger a new mempool report.                              | DCRDATA_MP_TRIGGER_TICKETS     |
| Dump to file the fees of all the tickets in mempool.                                                                     | DCRDATA_ENABLE_DUMP_ALL_MP_TIX |
| SQLite DB file name (default is dcrdata.sqlt.db)                                                                         | DCRDATA_SQLITE_DB_FILE_NAME    |
| Voting agendas DB file name (default is agendas.db)                                                                      | DCRDATA_AGENDA_DB_FILE_NAME    |
| Run in "Full Mode" mode, enables postgresql support                                                                      | DCRDATA_ENABLE_FULL_MODE       |
| PostgreSQL DB name.                                                                                                      | DCRDATA_PG_DB_NAME             |
| PostgreSQL DB user                                                                                                       | DCRDATA_POSTGRES_USER          |
| PostgreSQL DB password.                                                                                                  | DCRDATA_POSTGRES_PASS          |
| port or UNIX socket (e.g. /run/postgresql).                                                                              | DCRDATA_POSTGRES_HOST_URL      |
| Disable automatic dev fund balance query on new blocks.                                                                  | DCRDATA_DISABLE_DEV_PREFETCH   |
| Sync to the best block and exit. Do not start the explorer or API.                                                       | DCRDATA_ENABLE_SYNC_N_QUIT     |
| (experimental) Import side chain blocks from dcrd via the getchaintips RPC.                                              | DCRDATA_IMPORT_SIDE_CHAINS     |
| Daemon RPC user name                                                                                                     | DCRDATA_DCRD_USER              |
| Daemon RPC password                                                                                                      | DCRDATA_DCRD_PASS              |
| Hostname/IP and port of dcrd RPC server                                                                                  | DCRDATA_DCRD_URL               |
| File containing the dcrd certificate file                                                                                | DCRDATA_DCRD_CERT              |
| Disable TLS for the daemon RPC client                                                                                    | DCRDATA_DCRD_DISABLE_TLS       |

### Indexing the Blockchain

If dcrdata has not previously been run with the PostgreSQL database backend, it
is necessary to perform a bulk import of blockchain data and generate table
indexes. _This will be done automatically by `dcrdata`_ on a fresh startup.

Alternatively (but not recommended), the PostgreSQL tables may also be generated
with the `rebuilddb2` command line tool:

- Create the dcrdata user and database in PostgreSQL (tables will be created automatically).
- Set your PostgreSQL credentials and host in both `./cmd/rebuilddb2/rebuilddb2.conf`,
  and `dcrdata.conf` in the location specified by the `appdata` flag.
- Run `./rebuilddb2` to bulk import data and index the tables.
- In case of irrecoverable errors, such as detected schema changes without an
  upgrade path, the tables and their indexes may be dropped with `rebuilddb2 -D`.

Note that dcrdata requires that
[dcrd](https://docs.decred.org/getting-started/user-guides/dcrd-setup/) is
running with some optional indexes enabled. By default, these indexes are not
turned on when dcrd is installed. To enable them, set the following in
dcrd.conf:

```ini
txindex=1
addrindex=1
```

If these parameters are not set, dcrdata will be unable to retrieve transaction
details and perform address searches, and will exit with an error mentioning
these indexes.

### Starting dcrdata

Launch the dcrdata daemon and allow the databases to process new blocks. In
"lite" mode (without `--pg`), only a SQLite DB is populated, which usually
requires 30-60 minutes. In "full" mode (with `--pg`), concurrent synchronization
of both SQLite and PostgreSQL databases is performed, requiring from 3-12 hours.
See [System Hardware Requirements](#System-Hardware-Requirements) for more
information.

On subsequent launches, only blocks new to dcrdata are processed.

```bash
./dcrdata    # don't forget to configure dcrdata.conf in the appdata folder!
```

Unlike dcrdata.conf, which must be placed in the `appdata` folder or explicitly
set with `-C`, the "public" and "views" folders _must_ be in the same folder as
the `dcrdata` executable.

### Running the Web Interface During Synchronization

By default on dcrdata startup, syncing runs for all the blocks behind the current
best block height. Syncing status page with the syncing progress is the only
page that will run if `sync-status-limit` is not set in `dcrdata.conf` file.

When set with a value greater than 2 and less than 5000, all dcrdata pages will be
active on startup if and only if, the number of blocks behind the current best block
are less than the set `sync-status-limit` value.

For Example: If `sync-status-limit` is set to 1000, all dcrdata pages will be active
if only less than 1000 blocks need to be sync'd on startup otherwise only the sync status
page will be accesible till the syncing is complete.

```
sync-status-limit=1000
```

_It is recommended that you avoid setting `sync-status-limit` as a value larger than
1000 especially if your machine struggles handling dcrdata normal load. Setting a
larger value might worsen your situation especially when you try to load processor
intensive pages like ticketpool._

## System Hardware Requirements

The time required to sync in "full" mode varies greatly with system hardware and
software configuration. The most important factor is the storage medium on the
database machine. An SSD (preferably NVMe, not SATA) is strongly recommended if
you value your time and system performance.

### "lite" Mode (SQLite only)

Minimum:

- 1 CPU core
- 2 GB RAM
- HDD with 4GB free space

### "full" Mode (SQLite and PostgreSQL)

These specifications assume dcrdata and postgres are running on the same machine.

Minimum:

- 1 CPU core
- 4 GB RAM
- HDD with 60GB free space

Recommend:

- 2+ CPU cores
- 7+ GB RAM
- SSD (NVMe preferred) with 60 GB free space

If PostgreSQL is running on a separate machine, the minimum "lite" mode
requirements may be applied to the dcrdata machine, while the recommended
"full" mode requirements should be applied to the PostgreSQL host.

## dcrdata Daemon

The root of the repository is the `main` package for the `dcrdata` app, which
has several components including:

1. Block explorer (web interface).
2. Blockchain monitoring and data collection.
3. Mempool monitoring and reporting.
4. Database backend interfaces.
5. RESTful JSON API (custom and Insight) over HTTP(S).

### Block Explorer

After dcrdata syncs with the blockchain server via RPC, by default it will begin
listening for HTTP connections on `http://127.0.0.1:7777/`. This means it starts
a web server listening on IPv4 localhost, port 7777. Both the interface and port
are configurable. The block explorer and the JSON APIs are both provided by the
server on this port. See [JSON REST API](#json-rest-api) for details.

Note that while dcrdata can be started with HTTPS support, it is recommended to
employ a reverse proxy such as Nginx ("engine x"). See sample-nginx.conf for an
example Nginx configuration.

To save time and tens of gigabytes of disk storage space, dcrdata runs by
default in a reduced functionality ("lite") mode that does not require
PostgreSQL. To enable the PostgreSQL backend (and the expanded functionality),
dcrdata may be started with the `--pg` switch.

## APIs

The dcrdata block explorer is exposed by two APIs: a Decred implementation of the [Insight API](https://github.com/bitpay/insight-api) (EXPERIMENTAL), and its own JSON HTTP API. The Insight API uses the path prefix `/insight/api`. The dcrdata API uses the path prefix `/api`.

### Insight API (EXPERIMENTAL)

The [Insight API](https://github.com/bitpay/insight-api) is accessible via HTTP via REST or WebSocket. 

See the [Insight API documentation](api/Insight_API_documentation.md) for further details.

### dcrdata API

The dcrdata API is a REST API accessible via HTTP. To call the dcrdata API, use the `/api` path prefix.


#### Endpoint List

| Best block           | Path                   | Type                                  |
| -------------------- | ---------------------- | ------------------------------------- |
| Summary              | `/block/best`          | `types.BlockDataBasic`                |
| Stake info           | `/block/best/pos`      | `types.StakeInfoExtended`             |
| Header               | `/block/best/header`   | `dcrjson.GetBlockHeaderVerboseResult` |
| Hash                 | `/block/best/hash`     | `string`                              |
| Height               | `/block/best/height`   | `int`                                 |
| Size                 | `/block/best/size`     | `int32`                               |
| Subsidy              | `/block/best/subsidy`  | `types.BlockSubsidies`                |
| Transactions         | `/block/best/tx`       | `types.BlockTransactions`             |
| Transactions Count   | `/block/best/tx/count` | `types.BlockTransactionCounts`        |
| Verbose block result | `/block/best/verbose`  | `dcrjson.GetBlockVerboseResult`       |

| Block X (block index) | Path                  | Type                                  |
| --------------------- | --------------------- | ------------------------------------- |
| Summary               | `/block/X`            | `types.BlockDataBasic`                |
| Stake info            | `/block/X/pos`        | `types.StakeInfoExtended`             |
| Header                | `/block/X/header`     | `dcrjson.GetBlockHeaderVerboseResult` |
| Hash                  | `/block/X/hash`       | `string`                              |
| Size                  | `/block/X/size`       | `int32`                               |
| Subsidy               | `/block/best/subsidy` | `types.BlockSubsidies`                |
| Transactions          | `/block/X/tx`         | `types.BlockTransactions`             |
| Transactions Count    | `/block/X/tx/count`   | `types.BlockTransactionCounts`        |
| Verbose block result  | `/block/X/verbose`    | `dcrjson.GetBlockVerboseResult`       |

| Block H (block hash) | Path                     | Type                                  |
| -------------------- | ------------------------ | ------------------------------------- |
| Summary              | `/block/hash/H`          | `types.BlockDataBasic`                |
| Stake info           | `/block/hash/H/pos`      | `types.StakeInfoExtended`             |
| Header               | `/block/hash/H/header`   | `dcrjson.GetBlockHeaderVerboseResult` |
| Height               | `/block/hash/H/height`   | `int`                                 |
| Size                 | `/block/hash/H/size`     | `int32`                               |
| Subsidy              | `/block/best/subsidy`    | `types.BlockSubsidies`                |
| Transactions         | `/block/hash/H/tx`       | `types.BlockTransactions`             |
| Transactions count   | `/block/hash/H/tx/count` | `types.BlockTransactionCounts`        |
| Verbose block result | `/block/hash/H/verbose`  | `dcrjson.GetBlockVerboseResult`       |

| Block range (X < Y)                     | Path                      | Type                     |
| --------------------------------------- | ------------------------- | ------------------------ |
| Summary array for blocks on `[X,Y]`     | `/block/range/X/Y`        | `[]types.BlockDataBasic` |
| Summary array with block index step `S` | `/block/range/X/Y/S`      | `[]types.BlockDataBasic` |
| Size (bytes) array                      | `/block/range/X/Y/size`   | `[]int32`                |
| Size array with step `S`                | `/block/range/X/Y/S/size` | `[]int32`                |

| Transaction T (transaction id)      | Path            | Type              |
| ----------------------------------- | --------------- | ----------------- |
| Transaction details                 | `/tx/T`         | `types.Tx`        |
| Transaction details w/o block info  | `/tx/trimmed/T` | `types.TrimmedTx` |
| Inputs                              | `/tx/T/in`      | `[]types.TxIn`    |
| Details for input at index `X`      | `/tx/T/in/X`    | `types.TxIn`      |
| Outputs                             | `/tx/T/out`     | `[]types.TxOut`   |
| Details for output at index `X`     | `/tx/T/out/X`   | `types.TxOut`     |
| Vote info (ssgen transactions only) | `/tx/T/vinfo`   | `types.VoteInfo`  |
| Serialized bytes of the transaction | `/tx/hex/T`     | `string`          |
| Same as `/tx/trimmed/T`             | `/tx/decoded/T` | `types.TrimmedTx` |

| Transactions (batch)                                    | Path           | Type                |
| ------------------------------------------------------- | -------------- | ------------------- |
| Transaction details (POST body is JSON of `types.Txns`) | `/txs`         | `[]types.Tx`        |
| Transaction details w/o block info                      | `/txs/trimmed` | `[]types.TrimmedTx` |

| Address A                                                               | Path                           | Type                  |
| ----------------------------------------------------------------------- | ------------------------------ | --------------------- |
| Summary of last 10 transactions                                         | `/address/A`                   | `types.Address`       |
| Number and value of spent and unspent outputs                           | `/address/A/totals`            | `types.AddressTotals` |
| Verbose transaction result for last <br> 10 transactions                | `/address/A/raw`               | `types.AddressTxRaw`  |
| Summary of last `N` transactions                                        | `/address/A/count/N`           | `types.Address`       |
| Verbose transaction result for last <br> `N` transactions               | `/address/A/count/N/raw`       | `types.AddressTxRaw`  |
| Summary of last `N` transactions, skipping `M`                          | `/address/A/count/N/skip/M`    | `types.Address`       |
| Verbose transaction result for last <br> `N` transactions, skipping `M` | `/address/A/count/N/skip/Mraw` | `types.AddressTxRaw`  |

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

The full ticket pool endpoints accept the URL query `?sort=[true\|false]` for
requesting the tickets array in lexicographical order. If a sorted list or list
with deterministic order is _not_ required, using `sort=false` will reduce
server load and latency. However, be aware that the ticket order will be random,
and will change each time the tickets are requested.

<sup>\*</sup>For the pool info block range endpoint that accepts the `arrays` url query,
a value of `true` will put all pool values and pool sizes into separate arrays,
rather than having a single array of pool info JSON objects. This may make
parsing more efficient for the client.

| Vote and Agenda Info              | Path               | Type                        |
| --------------------------------- | ------------------ | --------------------------- |
| The current agenda and its status | `/stake/vote/info` | `dcrjson.GetVoteInfoResult` |

| Mempool                                           | Path                      | Type                            |
| ------------------------------------------------- | ------------------------- | ------------------------------- |
| Ticket fee rate summary                           | `/mempool/sstx`           | `apitypes.MempoolTicketFeeInfo` |
| Ticket fee rate list (all)                        | `/mempool/sstx/fees`      | `apitypes.MempoolTicketFees`    |
| Ticket fee rate list (N highest)                  | `/mempool/sstx/fees/N`    | `apitypes.MempoolTicketFees`    |
| Detailed ticket list (fee, hash, size, age, etc.) | `/mempool/sstx/details`   | `apitypes.MempoolTicketDetails` |
| Detailed ticket list (N highest fee rates)        | `/mempool/sstx/details/N` | `apitypes.MempoolTicketDetails` |

| Other                           | Path      | Type               |
| ------------------------------- | --------- | ------------------ |
| Status                          | `/status` | `types.Status`     |
| Coin Supply                     | `/supply` | `types.CoinSupply` |
| Endpoint list (always indented) | `/list`   | `[]string`         |

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

## Command Line Utilities

### rebuilddb

`rebuilddb` is a CLI app that performs a full blockchain scan that fills past
block data into a SQLite database. This functionality is included in the startup
of the dcrdata daemon, but may be called alone with rebuilddb.

### rebuilddb2

`rebuilddb2` is a CLI app used for maintenance of dcrdata's `dcrpg` database
(a.k.a. DB v2) that uses PostgreSQL to store a nearly complete record of the
Decred blockchain data. This functionality is included in the startup of the
dcrdata daemon, but may be called alone with rebuilddb. See the
[README.md](./cmd/rebuilddb2/README.md) for `rebuilddb2` for important usage
information.

### scanblocks

scanblocks is a CLI app to scan the blockchain and save data into a JSON file.
More details are in [its own README](./cmd/scanblocks/README.md). The repository
also includes a shell script, jsonarray2csv.sh, to convert the result into a
comma-separated value (CSV) file.

## Helper Packages

`package dcrdataapi` defines the data types, with json tags, used by the JSON
API. This facilitates authoring of robust Go clients of the API.

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

Packages `blockdata` and `dcrsqlite` are currently designed only for internal
use internal use by other dcrdata packages, but they may be of general value in
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

`dcrsqlite` defines:

- A `sql.DB` wrapper type (`DB`) with the necessary SQLite queries for
  storage and retrieval of block and stake data.
- The `wiredDB` type, intended to satisfy the `DataSourceLite` interface used by
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

See the GitHub issue tracker and the [project milestones](https://github.com/decred/dcrdata/milestones).

## Contributing

Yes, please! See the CONTRIBUTING.md file for details, but here's the gist of it:

1. Fork the repo.
2. Create a branch for your work (`git branch -b cool-stuff`).
3. Code something great.
4. Commit and push to your repo.
5. Create a [pull request](https://github.com/decred/dcrdata/compare).

**DO NOT merge from master to your feature branch; rebase.**

Before committing any changes to the Gopkg.lock file, you must update `dep` to
the latest version via:

    go get -u github.com/go/dep/cmd/dep

**To update `dep` from the network, it is important to use the `-u` flag as
shown above.**

Note that all dcrdata.org community and team members are expected to adhere to
the code of conduct, described in the CODE_OF_CONDUCT file.

Also, [come chat with us on Slack](https://slack.decred.org/)!

## License

This project is licensed under the ISC License. See the [LICENSE](LICENSE) file for details.
