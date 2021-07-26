#!/usr/bin/env bash

# usage:
# ./run_tests.sh
#
# To use build tags:
#  TESTTAGS="chartdata" ./run_tests.sh
#  TESTTAGS="pgonline" ./run_tests.sh
#  TESTTAGS="pgonline fullpgdb" ./run_tests.sh

set -ex

REPO=dcrdata

go version

if [[ -v TESTTAGS ]]; then
  TESTTAGS="-tags \"${TESTTAGS}\""
elsif
  TESTTAGS=
fi

# Check tests
TMPDIR=$(mktemp -d)
git clone https://github.com/dcrlabs/bug-free-happiness $TMPDIR/test-data-repo

if [[ $TESTTAGS =~ "pgonline" || $TESTTAGS =~ "chartdata" ]]; then
  mkdir -p ./testutil/dbconfig/test.data
  BLOCK_RANGE="0-199"
  tar xvf $TMPDIR/test-data-repo/pgdb/pgsql_"$BLOCK_RANGE".tar.xz -C ./testutil/dbconfig/test.data

  # Set up the tests db.
  psql -U postgres -c "DROP DATABASE IF EXISTS dcrdata_mainnet_test"
  psql -U postgres -c "CREATE DATABASE dcrdata_mainnet_test"

  # Pre-populate the pg db with test data.
  ./testutil/dbload/dbload
fi

tar xvf $TMPDIR/test-data-repo/stakedb/test_ticket_pool.bdgr.tar.xz -C ./stakedb
tar xvf $TMPDIR/test-data-repo/stakedb/test_ticket_pool_v1.bdgr.tar.xz -C ./stakedb

# run tests on all modules
for i in $(find . -name go.mod -type f -print); do
  module=$(dirname ${i})
  echo "==> ${module}"
  (cd ${module} && \
    go test $TESTTAGS ./... && \
    golangci-lint run --deadline=10m \
      --out-format=github-actions \
      --disable-all \
      --enable govet \
      --enable staticcheck \
      --enable gosimple \
      --enable unconvert \
      --enable ineffassign \
      --enable structcheck \
      --enable goimports \
      --enable misspell \
      --enable unparam \
      --enable asciicheck \
      --enable makezero \
  )
done

if [[ $TESTTAGS =~ "pgonline" || $TESTTAGS =~ "chartdata" ]]; then
  # Drop the tests db.
  psql -U postgres -c "DROP DATABASE IF EXISTS dcrdata_mainnet_test"
fi

echo "------------------------------------------"
echo "Tests completed successfully!"

# Remove all the tests data
rm -rf $TMPDIR $TMPFILE
rm -rf ./stakedb/pooldiffs.bdgr ./stakedb/test_ticket_pool.bdgr ./stakedb/test_ticket_pool_v1.bdgr ./testutil/dbconfig/test.data
