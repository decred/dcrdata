#!/bin/bash
# For example,
# jq -r 'map(.ticket_pool)' fullscan.json | ./jsonarray2csv.sh ticketpool.csv

in=`tee`
# echo $in

echo $in | jq -r '(.[0] | keys_unsorted) as $keys | $keys, map([.[ $keys[] ]])[] | @csv' > $1
