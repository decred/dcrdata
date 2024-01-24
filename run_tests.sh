#!/usr/bin/env bash

# usage:
# ./run_tests.sh
#
# To use build tags:
#  TESTTAGS="chartdata" ./run_tests.sh
#  TESTTAGS="pgonline" ./run_tests.sh
#  TESTTAGS="pgonline fullpgdb" ./run_tests.sh

set -ex

GV=$(go version | sed "s/^.*go\([0-9.]*\).*/\1/")
echo "Go version: $GV"

if [[ -v TESTTAGS ]]; then
  TESTTAGS="-tags \"${TESTTAGS}\""
else
  TESTTAGS=
fi

# Check tests
TMPDIR=$(mktemp -d)
git clone https://github.com/dcrlabs/bug-free-happiness "$TMPDIR/test-data-repo"

if [[ $TESTTAGS =~ "pgonline" || $TESTTAGS =~ "chartdata" ]]; then
  mkdir -p ./testutil/dbconfig/test.data
  BLOCK_RANGE="0-199"
  tar xvf "$TMPDIR/test-data-repo/pgdb/pgsql_$BLOCK_RANGE.tar.xz" -C ./testutil/dbconfig/test.data

  # Set up the tests db.
  psql -U postgres -c "DROP DATABASE IF EXISTS dcrdata_mainnet_test"
  psql -U postgres -c "CREATE DATABASE dcrdata_mainnet_test"

  # Pre-populate the pg db with test data.
  ./testutil/dbload/dbload
fi

tar xvf "$TMPDIR/test-data-repo/stakedb/test_ticket_pool.bdgr.tar.xz" -C ./stakedb
tar xvf "$TMPDIR/test-data-repo/stakedb/test_ticket_pool_v1.bdgr.tar.xz" -C ./stakedb

# Do the module paths in order so that go mod tidy updates will cascade to
# dependent modules.
MODPATHS="./go.mod ./exchanges/go.mod ./gov/go.mod ./db/dcrpg/go.mod ./cmd/dcrdata/go.mod \
    ./pubsub/democlient/go.mod ./cmd/swapscan-btc/go.mod ./testutil/dbload/go.mod \
    ./testutil/apiload/go.mod ./exchanges/rateserver/go.mod"
#MODPATHS=$(find . -name go.mod -type f -print)

ROOT=$PWD
# run tests on all modules
for MODPATH in $MODPATHS; do
  module=$(dirname "$MODPATH")
  echo "==> ${module}"
  (cd "${module}"
    go test $TESTTAGS ./...
    golangci-lint run -c ${ROOT}/.golangci.yml
    if [[ "$GV" =~ ^1.21 ]]; then
      MOD_STATUS=$(git status --porcelain go.mod go.sum)
      go mod tidy
      UPDATED_MOD_STATUS=$(git status --porcelain go.mod go.sum)
      if [ "$UPDATED_MOD_STATUS" != "$MOD_STATUS" ]; then
        echo "$module: running 'go mod tidy' modified go.mod and/or go.sum"
      git diff --unified=0 go.mod go.sum
        exit 1
      fi
    fi
  )
done

if [[ $TESTTAGS =~ "pgonline" || $TESTTAGS =~ "chartdata" ]]; then
  # Drop the tests db.
  psql -U postgres -c "DROP DATABASE IF EXISTS dcrdata_mainnet_test"
fi

echo "------------------------------------------"
echo "Tests completed successfully!"

# Remove all the tests data
rm -rf "$TMPDIR" "$TMPFILE"
rm -rf ./stakedb/pooldiffs.bdgr ./stakedb/test_ticket_pool.bdgr ./stakedb/test_ticket_pool_v1.bdgr ./testutil/dbconfig/test.data
