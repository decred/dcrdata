# `package dcrpg`

The `dcrpg` package provides types and functions for manipulating PostgreSQL tables, and storing blocks, transactions, inputs, and outputs.

## Performance and Bulk Loading

When performing a bulk data import, it is wise to first drop any existing indexes and create them again after insertion is completed.  Functions are provided to create and drop the indexes.

PostgreSQL performance will be poor, particuarly during bulk import, unless synchronous transaction commits are disabled via the `synchronous_commit = off` configuration setting in your postgresql.conf. There are numerous [PostreSQL tuning settings](https://wiki.postgresql.org/wiki/Tuning_Your_PostgreSQL_Server), but a quick suggestion for your system can be provided by [PgTune](http://pgtune.leopard.in.ua/). During [initial table population](https://wiki.postgresql.org/wiki/Bulk_Loading_and_Restores), it is also OK to turn off autovacuum, full page writes, and possibly fsync. Remember to change settings back to production ready values.
