# RateServer

RateServer is a central server for distributing Decred exchange rate data to
any number of clients. It is particularly useful for DCRData instances behind a
load balancer, though certainly not restricted to that application.

### Build and Run

For a module-enabled version of Go, getting started should be as simple as

```
cd path/to/dcrdata/dcrrates/rateserver
go build
./rateserver
```

See [DCRData's notes on Go](https://github.com/decred/dcrdata#building-dcrdata-with-go-111)
for additional configuration details.

### Using RateServer with DCRData

To point DCRData to RateServer, you must specify DCRData's `ratemaster` and
`ratecert` configuration options. `exchange-monitor` must also be enabled. Set
`ratemaster` to the same value as RateServer's `listen` parameter (defaults to
port 7778).

If you do not specify a path to a TLS key-certificate pair, they will be
generated in the application home directory (`appdir`). By default, the
generated certificate is only valid for requests to localhost. To specify
additional valid domain names for the generated certificate, delete any existing
auto-generated certificate, and restart RateServer with the `altdnsnames`
parameter. The `altdnsnames` parameter can be specified multiple times.

### Options
```
-c, --config=            Path to a custom configuration file.
    --appdir=            Path to application home directory. (~/.dcrrates)
-l, --listen=            gRPC listen address. (default: :7778)
    --logpath=           Directory to log output. ([appdir]/logs/)
    --loglevel=          Logging level {trace, debug, info, warn, error, critical}
    --disable-exchange=  Exchanges to disable. See /exchanges/exchanges.go for available exchanges. Use a comma to separate multiple exchanges
    --exchange-currency= The default bitcoin price index. A 3-letter currency code. (default: USD)
    --exchange-refresh=  Time between API calls for exchange data. See (ExchangeBotConfig).DataExpiry. (default: 20m)
    --exchange-expiry=   Maximum age before exchange data is discarded. See (ExchangeBotConfig).RequestExpiry. (default: 60m)
    --tlscert=           Path to the TLS certificate. Will be created if it doesn't already exist. ([appdir]/rpc.cert)
    --tlskey=            Path to the TLS key. Will be created if it doesn't already exist. ([appdir]/rpc.key)
    --altdnsnames=       Specify additional dns names to use when generating the rpc server certificate
-h, --help               Show this help message
```
