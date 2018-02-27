#!/usr/bin/env bash
set -ex

# The script does automatic checking on a Go package and its sub-packages,
# including:
# 1. gofmt         (http://golang.org/cmd/gofmt/)
# 2. go vet        (http://golang.org/cmd/vet)
# 3. gosimple      (https://github.com/dominikh/go-simple)
# 4. unconvert     (https://github.com/mdempsky/unconvert)
# 5. ineffassign   (https://github.com/gordonklaus/ineffassign)
# 6. race detector (http://blog.golang.org/race-detector)

# gometalinter (github.com/alecthomas/gometalinter) is used to run each each
# static checker.

testrepo () {
  TMPFILE=$(mktemp)

  # Check lockfile
  cp Gopkg.lock $TMPFILE && dep ensure && diff Gopkg.lock $TMPFILE >/dev/null
  if [ $? != 0 ]; then
    echo 'lockfile must be updated with dep ensure'
    exit 1
  fi

  # Check linters
  gometalinter --vendor --disable-all --deadline=10m \
    --enable=gofmt \
    --enable=vet \
    --enable=gosimple \
    --enable=unconvert \
    --enable=ineffassign \
    ./...
  if [ $? != 0 ]; then
    echo 'gometalinter has some complaints'
    exit 1
  fi

  # Test application install
  if [ $GOVERSION == 1.10 ]; then
    go install -i . ./cmd/...
  else
    go install . ./cmd/...
  fi
  if [ $? != 0 ]; then
    echo 'go install failed'
    exit 1
  fi

  # Check tests
  env GORACE='halt_on_error=1' go test -race ./...
  if [ $? != 0 ]; then
    echo 'go tests failed'
    exit 1
  fi

  echo "------------------------------------------"
  echo "Tests completed successfully!"
}

GOVERSION=$(go version | awk '{print $3}' | tr -d 'go' | cut -f1,2 -d.)
testrepo
