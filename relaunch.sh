#!/bin/sh

echo 'Stopping dcrdata...'
killall -w -INT dcrdata
sleep 1

echo 'Rebuilding...'
cd $GOPATH/src/github.com/dcrdata/dcrdata
git checkout master
git pull --ff-only origin master
go build -v

echo 'Launching!'
./dcrdata
