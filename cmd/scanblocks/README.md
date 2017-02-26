# scanblocks

This is a command line utility that scans the entire blockchain, up to the best
block syncd by dcrd, to extract basic data from each block.

## Usage

You just need to specify the user, password, host:port, and (optionally) the RPC
TLS certificate if using a TLS connection to dcrd.

```sh
./scanblocks -user dcrduser -pass dcrdpass
```

The argument `-notls` is true by default, but if you set it to false, it is
required to specify the RPC cert for dcrd with `-cert`.  I strongly suggest to
run dcrd with `--notls` when using `scanblocks` for performance reasons.

There is no config file. Options must be specified on the command line.

## Output

The output is a JSON file called "fullscan.json" that contains an array of JSON
objects of the form:

```json
{
  "height": 7235,
  "size": 2234,
  "hash": "000000000000029f185cc6863a45319b9abcf379f858494c0fa5de401dcba3c0",
  "diff": 283465.55356867,
  "sdiff": 10.48011259,
  "time": 1457034473,
  "ticket_pool": {
    "size": 41290,
    "value": 115413.70819395,
    "valavg": 2.7951975828033424
  }
}
```

**NOTE**: This is likely to change in the future.

There is a covenience script calle jsonarray2csv.sh that may be used to convert
JSON data into a csv file.

## Details

First, `scanblocks` gathers basic data, not including ticket pool values, in a
quick scan. Next, it creates a temporary stake database, which allows to query
the live tickets at any given height. Then the value of each live ticket in the
pool is collected, cached, and used to compute the total ticket pool value and
average live ticket value.  Finally, the data is encoded in JSON and stored to
the file system.

## License

ISC License

Copyright (c) 2017, Jonathan Chappelow

See LICENSE file in this repository for details.
