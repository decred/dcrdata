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

### TLS

When starting RateServer, if a TLS certificate and key cannot be located at the
paths specified by `tlscert` and `tlskey`, they will be auto-generated and saved
to those locations. By default, the generated certificate is only valid for
requests to `localhost`. To specify additional host names, delete or move any
existing certificate and key, and restart RateServer with the `altdnsnames`
parameter. `altdnsnames` can be specified multiple times.

### Using RateServer with DCRData

To point DCRData to RateServer, you must set DCRData's `ratemaster` and
`ratecert` configuration options. `exchange-monitor` must also be enabled. You
must specify a host name, whether it be `localhost`, an IP address, or a public
domain name. The supplied host name should match a name in RateServer's TLS
configuration.

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

All options (except `--config`) can be set via either the CLI or an INI
configuration file (see sample-rateserver.conf).
