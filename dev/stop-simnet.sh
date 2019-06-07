#!/usr/bin/env bash

TMUX_SESS="dcrd-parallel-nodes"

tmux has-session -t ${TMUX_SESS} &> /dev/null
if [ $? -eq 1 ]; then
    echo "tmux session \"${TMUX_SESS}\" does not exist."
    exit 1
fi

PANEIDS=(`tmux list-panes -s -t ${TMUX_SESS} -F "#{pane_id}"`)

echo "Stopping simnet nodes and wallets..."
for pi in ${PANEIDS[@]}
do
    tmux send-keys -t "$pi" C-c
done
sleep 4
for pi in ${PANEIDS[@]}
do
    #tmux send-keys -t "$pi" C-d
    tmux kill-pane -t "$pi"
done

tmux has-session -t ${TMUX_SESS} &> /dev/null
if [ $? -eq 1 ]; then
    echo "Successfully closed nodes, wallets, and tmux sessions."
else
    echo "Failed to close all tmux sessions. Close them manually."
fi
