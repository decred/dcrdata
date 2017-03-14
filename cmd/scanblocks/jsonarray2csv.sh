#!/bin/bash
# jsonarray2csv.sh - Convert any homogeneous JSON array into a CSV table.
#    Requires jq (https://stedolan.github.io/jq/).
#
# For example,
# jq -r 'map(.ticket_pool)' fullscan.json | ./jsonarray2csv.sh ticketpool.csv
#
# Copyright (c) 2016 Jonathan Chappelow.
# ISC licensed (see the LICENSE file at the root of the repository).

in=`tee`
# echo $in

echo $in | jq -r '(.[0] | keys_unsorted) as $keys | $keys, map([.[ $keys[] ]])[] | @csv' > $1
