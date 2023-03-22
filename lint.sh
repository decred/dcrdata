#!/usr/bin/env bash
#
# Lint all modules with golangci-lint.
#
#   lint.sh - lints all modules in the repository root, or the current folder if
#   not in a git workspace.
#
#   lint.sh [folder] - lints all modules in the specified folder.

if ! REPO=$(git rev-parse --show-toplevel 2> /dev/null); then
    REPO=$(pwd)
fi

# Root is the input argument, or REPO if no input argument.
ROOT=${1:-$REPO}
echo "Linting all modules under ${ROOT}"

set -e

shopt -s expand_aliases

export GO111MODULE=on

GV=$(go version | sed "s/^.*go\([0-9.]*\).*/\1/")
echo "Go version: $GV"

pushd "$ROOT" > /dev/null

# Do the module paths in order so that go mod tidy updates will cascade to
# dependent modules.
MODPATHS="./go.mod ./exchanges/go.mod ./gov/go.mod ./db/dcrpg/go.mod ./cmd/dcrdata/go.mod \
    ./pubsub/democlient/go.mod ./cmd/swapscan-btc/go.mod ./testutil/dbload/go.mod \
    ./testutil/apiload/go.mod ./exchanges/rateserver/go.mod"
#MODPATHS=$(find . -name go.mod -type f -print)

alias superlint="golangci-lint run --deadline=10m \
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
    --enable makezero"

# run lint on all listed modules
set +e
ERROR=0
trap 'ERROR=$?' ERR
for MODPATH in $MODPATHS; do
    module=$(dirname "${MODPATH}")
    pushd "$module" > /dev/null
    echo "Linting: $MODPATH"
    superlint
    if [[ "$GV" =~ ^1.20 ]]; then
		MOD_STATUS=$(git status --porcelain go.mod go.sum)
		go mod tidy
		UPDATED_MOD_STATUS=$(git status --porcelain go.mod go.sum)
		if [ "$UPDATED_MOD_STATUS" != "$MOD_STATUS" ]; then
			echo "$module: running 'go mod tidy' modified go.mod and/or go.sum"
		git diff --unified=0 go.mod go.sum
			exit 1
		fi
	fi
    popd > /dev/null
done

popd > /dev/null # ROOT

if [ $ERROR -ne 0 ]; then
    exit $ERROR
fi
