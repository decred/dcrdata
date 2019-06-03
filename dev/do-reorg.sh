#!/usr/bin/env bash

set -e

SESSION="dcrd-parallel-nodes"

# mine some on both nodes
tmux send-key -t dcrd-parallel-nodes:0.0 "./mine-both" C-m

sleep 6

# disconnect b from a
tmux send-key -t dcrd-parallel-nodes:2.1 "./ctl addnode 127.0.0.1:19100 remove" C-m

sleep 2

# mine a block on node a
tmux send-key -t dcrd-parallel-nodes:1.1 "./mine" C-m

# mine 3 blocks on node b
tmux send-key -t dcrd-parallel-nodes:2.1 "./mine" C-m
tmux send-key -t dcrd-parallel-nodes:2.1 "./mine" C-m
tmux send-key -t dcrd-parallel-nodes:2.1 "./mine" C-m

sleep 4

# reconnect b to a
tmux send-key -t dcrd-parallel-nodes:2.1 "./ctl addnode 127.0.0.1:19100 add" C-m

sleep 2

grep REORG ~/dcrdsimnet/alpha/log/simnet/dcrd.log
