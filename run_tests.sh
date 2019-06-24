#!/usr/bin/env bash

# usage:
# ./run_tests.sh                         # local, go 1.12
# ./run_tests.sh docker                  # docker, go 1.12
# ./run_tests.sh podman                  # podman, go 1.12
#
# To use build tags:
#  TESTTAGS="pgonline chartdata" ./run_tests.sh

set -ex

# Default GOVERSION
[[ ! "$GOVERSION" ]] && GOVERSION=1.12
REPO=dcrdata

if [[ -v TESTTAGS ]]; then
  TESTTAGSWITCH=-tags
fi

testrepo () {
  TMPDIR=$(mktemp -d)
  TMPFILE=$(mktemp)
  export GO111MODULE=on

  go version

  ROOTPATH=$(go list -m -f {{.Dir}} 2>/dev/null)
  ROOTPATHPATTERN=$(echo $ROOTPATH | sed 's/\\/\\\\/g' | sed 's/\//\\\//g')
  MODPATHS=$(go list -m -f {{.Dir}} all 2>/dev/null | grep "^$ROOTPATHPATTERN")

  # Test application install
  go build
  (cd cmd/rebuilddb && go build)
  (cd cmd/rebuilddb2 && go build)
  (cd cmd/scanblocks && go build)
  (cd pubsub/democlient && go build)
  (cd testutil/apiload && go build)
  (cd testutil/dbload && go build)

  # Check tests
  git clone https://github.com/dcrlabs/bug-free-happiness $TMPDIR/test-data-repo
  
  if [[ $TESTTAGS =~ "pgonline" ]]; then
    mkdir -p ./testutil/dbconfig/test.data
    BLOCK_RANGE="0-199"
    tar xvf $TMPDIR/test-data-repo/sqlitedb/sqlite_"$BLOCK_RANGE".tar.xz -C ./testutil/dbconfig/test.data
    tar xvf $TMPDIR/test-data-repo/pgdb/pgsql_"$BLOCK_RANGE".tar.xz -C ./testutil/dbconfig/test.data

    # Set up the tests db.
    psql -U postgres -c "DROP DATABASE IF EXISTS dcrdata_mainnet_test"
    psql -U postgres -c "CREATE DATABASE dcrdata_mainnet_test"

    # Pre-populate the pg db with test data.
    ./testutil/dbload/dbload
  fi

  tar xvf $TMPDIR/test-data-repo/stakedb/test_ticket_pool.bdgr.tar.xz -C ./stakedb

  # run tests on all modules
  for MODPATH in $MODPATHS; do
    env GORACE='halt_on_error=1' go test -v $TESTTAGSWITCH "$TESTTAGS" $(cd $MODPATH && go list -m)/...
  done

  # check linters
  ./lint.sh

  if [[ $TESTTAGS =~ "pgonline" ]]; then
  # Drop the tests db.
  psql -U postgres -c "DROP DATABASE IF EXISTS dcrdata_mainnet_test"
  fi

  # webpack
  npm install
  npm run build

  echo "------------------------------------------"
  echo "Tests completed successfully!"

  # Remove all the tests data
  rm -rf $TMPDIR $TMPFILE
  rm -rf ./stakedb/pooldiffs.bdgr ./stakedb/test_ticket_pool.bdgr ./testutil/dbconfig/test.data
}

DOCKER=
[[ "$1" == "docker" || "$1" == "podman" ]] && DOCKER=$1
if [ ! "$DOCKER" ]; then
    testrepo
    exit
fi

DOCKER_IMAGE_TAG=dcrdata-golang-builder-$GOVERSION
$DOCKER pull decred/$DOCKER_IMAGE_TAG

$DOCKER run --rm -it -v $(pwd):/src decred/$DOCKER_IMAGE_TAG /bin/bash -c "\
  rsync -ra --include-from=<(git --git-dir=/src/.git ls-files) \
  --filter=':- .gitignore' \
  /src/ /go/src/github.com/decred/$REPO/ && \
  cd github.com/decred/$REPO/ && \
  env GOVERSION=$GOVERSION GO111MODULE=on bash run_tests.sh"
