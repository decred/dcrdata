#!/bin/sh
#
# Tmux script that sets up a dcrdata simnet mining harness.
#
# The script makes a few assumptions about the system it is running on:
# - tmux is installed
# - dcrd, dcrwallet, dcrctl, dcrdata and available on $PATH
#
#             alpha  <------>  beta
#    listen   19100           19200
# rpclisten   19101 <.     .> 19201
#                aw |      |  bw
# rpclisten   19102          19202

set -e

TMUX_SESSION="dcrdata-harness"
HARNESS_ROOT=~/dcrdsimnet
RPC_USER="USER"
RPC_PASS="PASS"
WALLET_PASS=123
ALPHA="alpha"
BETA="beta"
ALPHA_WALLET="aw"
BETA_WALLET="bw"
ALPHA_WALLET_SEED="1111111111111111111111111111111111111111111111111111111111111111"
BETA_WALLET_SEED="2222222222222222222222222222222222222222222222222222222222222222"
ALPHA_WALLET_MININGADDR="Ssoaqgx4ecmHX54LqrUXgqi6miUFxP9iUvc"
BETA_WALLET_MININGADDR="SsgkhRgr9JdonELE7MjK8qUkwSPsrTnWcE6"
ALPHA_WALLET_SEND_TO_ADDR="SsnHu9PaK563xG4KwSKc53p1v7E48q5jvvz"
BETA_WALLET_SEND_TO_ADDR="SsnhVyWxY6c5xEztSBb9xBqf9gdjEHpyCDx"

if [ -d "${HARNESS_ROOT}" ] ; then
  rm -R "${HARNESS_ROOT}"
fi

mkdir -p "${HARNESS_ROOT}/${ALPHA}" 
mkdir -p "${HARNESS_ROOT}/${BETA}" 
mkdir -p "${HARNESS_ROOT}/${ALPHA_WALLET}" 
mkdir -p "${HARNESS_ROOT}/${BETA_WALLET}"

echo "Writing node config files"

cat > "${HARNESS_ROOT}/dcrd.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
simnet=1
debuglevel=INFO
txindex=1
addrindex=1
EOF

cat > "${HARNESS_ROOT}/${ALPHA}/dcractl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=./rpc.cert
rpcserver=127.0.0.1:19101
simnet=1
EOF

cat > "${HARNESS_ROOT}/${BETA}/dcrbctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=./rpc.cert
rpcserver=127.0.0.1:19201
simnet=1
EOF

cat > "${HARNESS_ROOT}/${ALPHA_WALLET}/wallet.conf" <<EOF
username=${RPC_USER}
password=${RPC_PASS}
cafile=${HARNESS_ROOT}/${ALPHA}/rpc.cert
appdata=${HARNESS_ROOT}/${ALPHA_WALLET}
rpclisten=127.0.0.1:19102
rpcconnect=127.0.0.1:19101
pass=${WALLET_PASS}
enablevoting=1
enableticketbuyer=1
ticketbuyer.limit=10
accountgaplimit=50
simnet=1
nogrpc=1
EOF

cat > "${HARNESS_ROOT}/${BETA_WALLET}/wallet.conf" <<EOF
username=${RPC_USER}
password=${RPC_PASS}
cafile=${HARNESS_ROOT}/${BETA}/rpc.cert
appdata=${HARNESS_ROOT}/${BETA_WALLET}
rpclisten=127.0.0.1:19202
rpcconnect=127.0.0.1:19201
pass=${WALLET_PASS}
enablevoting=1
enableticketbuyer=1
ticketbuyer.limit=10
accountgaplimit=50
simnet=1
nogrpc=1
EOF

cat >> "${HARNESS_ROOT}/${ALPHA_WALLET}/awctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=./rpc.cert
walletrpcserver=127.0.0.1:19102
simnet=1
EOF

cat >> "${HARNESS_ROOT}/${BETA_WALLET}/bwctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=./rpc.cert
walletrpcserver=127.0.0.1:19202
simnet=1
EOF

cd ${HARNESS_ROOT} && tmux new-session -d -s $TMUX_SESSION

################################################################################
# Setup the alpha node.
################################################################################
cat > "${HARNESS_ROOT}/${ALPHA}/ctl" <<EOF
#!/bin/sh
dcrctl -C dcractl.conf \$*
EOF
chmod +x "${HARNESS_ROOT}/${ALPHA}/ctl"

tmux rename-window -t $TMUX_SESSION 'alpha'
tmux send-keys "cd ${HARNESS_ROOT}/${ALPHA}" C-m

echo "Starting alpha node"
tmux send-keys "dcrd -C ../dcrd.conf --listen 127.0.0.1:19100 \
--appdata=${HARNESS_ROOT}/${ALPHA} --rpclisten 127.0.0.1:19101 \
--whitelist=127.0.0.1 --miningaddr ${ALPHA_WALLET_MININGADDR}" C-m

################################################################################
# Setup the alpha node's dcrctl (actl).
################################################################################
cat > "${HARNESS_ROOT}/${ALPHA}/mine" <<EOF
#!/bin/sh
  NUM=1
  case \$1 in
      ''|*[!0-9]*)  ;;
      *) NUM=\$1 ;;
  esac
  for i in \$(seq \$NUM) ; do
    dcrctl -C dcractl.conf generate 1
    sleep 0.3
  done
EOF
chmod +x "${HARNESS_ROOT}/${ALPHA}/mine"

# send some coins to the beta wallet and mine a block on alpha.
cat > "${HARNESS_ROOT}/${ALPHA}/advance" <<EOF
#!/bin/sh
NUM=1
  case \$1 in
      ''|*[!0-9]*)  ;;
      *) NUM=\$1 ;;
  esac
  for i in \$(seq \$NUM) ; do
    cd ${HARNESS_ROOT}/${ALPHA_WALLET} && ./ctl sendfrom default ${BETA_WALLET_SEND_TO_ADDR} 3
    cd ${HARNESS_ROOT}/${ALPHA} && ./mine 1
  done

EOF
chmod +x "${HARNESS_ROOT}/${ALPHA}/advance"

tmux new-window -t $TMUX_SESSION -n 'actl'
tmux send-keys "cd ${HARNESS_ROOT}/${ALPHA}" C-m

#mine 60 blocks on alpha.
sleep 1
echo "Mining 60 blocks on alpha"
tmux send-keys "./mine 60; tmux wait -S mine-alpha" C-m
tmux wait mine-alpha

################################################################################
# Setup the alpha wallet.
################################################################################
cat > "${HARNESS_ROOT}/${ALPHA_WALLET}/ctl" <<EOF
#!/bin/sh
dcrctl -C awctl.conf --wallet \$*
EOF
chmod +x "${HARNESS_ROOT}/${ALPHA_WALLET}/ctl"

tmux new-window -t $TMUX_SESSION -n 'aw'
tmux send-keys "cd ${HARNESS_ROOT}/${ALPHA_WALLET}" C-m
echo "Creating alpha wallet"
tmux send-keys "dcrwallet -C wallet.conf --create <<EOF
y
n
y
${ALPHA_WALLET_SEED}
EOF" C-m
sleep 1
tmux send-keys "dcrwallet -C wallet.conf" C-m 

################################################################################
# Setup the alpha wallet's dcrctl (awctl).
################################################################################
sleep 3
tmux new-window -t $TMUX_SESSION -n 'awctl'
tmux send-keys "cd ${HARNESS_ROOT}/${ALPHA_WALLET}" C-m
tmux send-keys "./ctl getnewaddress default" C-m # alpha send-to address.
tmux send-keys "./ctl getbalance" C-m

################################################################################
# Setup the beta node.
################################################################################
cat > "${HARNESS_ROOT}/${BETA}/ctl" <<EOF
#!/bin/sh
dcrctl -C dcrbctl.conf \$*
EOF
chmod +x "${HARNESS_ROOT}/${BETA}/ctl"

tmux new-window -t $TMUX_SESSION -n 'beta'
tmux send-keys "cd ${HARNESS_ROOT}/${BETA}" C-m

echo "Starting beta node"
tmux send-keys "dcrd -C ../dcrd.conf --appdata=${HARNESS_ROOT}/${BETA} \
--whitelist=127.0.0.1 --connect=127.0.0.1:19100 \
--listen=127.0.0.1:19200 --rpclisten=127.0.0.1:19201 \
--miningaddr=${BETA_WALLET_MININGADDR}" C-m

################################################################################
# Setup the beta node's dcrctl (bctl).
################################################################################
cat > "${HARNESS_ROOT}/${BETA}/mine" <<EOF
#!/bin/sh
  NUM=1
  case \$1 in
      ''|*[!0-9]*)  ;;
      *) NUM=\$1 ;;
  esac
  for i in \$(seq \$NUM) ; do
    dcrctl -C dcrbctl.conf generate 1
    sleep 0.3
  done
EOF
chmod +x "${HARNESS_ROOT}/${BETA}/mine"

# send some coins to the alpha wallet and mine on beta.
cat > "${HARNESS_ROOT}/${BETA}/advance" <<EOF
#!/bin/sh
  NUM=1
  case \$1 in
      ''|*[!0-9]*)  ;;
      *) NUM=\$1 ;;
  esac
  for i in \$(seq \$NUM) ; do
    cd ${HARNESS_ROOT}/${BETA_WALLET} && ./ctl sendfrom default ${ALPHA_WALLET_SEND_TO_ADDR} 5
    cd ${HARNESS_ROOT}/${BETA} && ./mine 1
  done
EOF
chmod +x "${HARNESS_ROOT}/${BETA}/advance"

# force a reorganization.
cat > "${HARNESS_ROOT}/${BETA}/reorg" <<EOF
#!/usr/bin/env bash
./ctl node remove 127.0.0.1:19100
./mine 1
cd "${HARNESS_ROOT}/${ALPHA}"
./mine 2
cd "${HARNESS_ROOT}/${BETA}"
./ctl node connect 127.0.0.1:19100 perm
EOF
chmod +x "${HARNESS_ROOT}/${BETA}/reorg"

tmux new-window -t $TMUX_SESSION -n 'bctl'
tmux send-keys "cd ${HARNESS_ROOT}/${BETA}" C-m

# Mine 60 blocks on beta.
sleep 1
echo "Mining 60 blocks on beta"
tmux send-keys "./mine 60; tmux wait -S mine-beta" C-m
tmux wait mine-beta

################################################################################
# Setup the beta wallet.
################################################################################
cat > "${HARNESS_ROOT}/${BETA_WALLET}/ctl" <<EOF
#!/bin/sh
dcrctl -C bwctl.conf --wallet \$*
EOF
chmod +x "${HARNESS_ROOT}/${BETA_WALLET}/ctl"

tmux new-window -t $TMUX_SESSION -n 'bw'
tmux send-keys "cd ${HARNESS_ROOT}/${BETA_WALLET}" C-m
echo "Creating beta wallet"
tmux send-keys "dcrwallet -C wallet.conf --create <<EOF
y
n
y
${BETA_WALLET_SEED}
EOF" C-m
tmux send-keys "dcrwallet -C wallet.conf" C-m 

################################################################################
# Setup the beta wallet's dcrctl (bwctl).
################################################################################
sleep 3
tmux new-window -t $TMUX_SESSION -n 'bwctl'
tmux send-keys "cd ${HARNESS_ROOT}/${BETA_WALLET}" C-m
tmux send-keys "./ctl getnewaddress default" C-m # beta send-to address.
tmux send-keys "./ctl getbalance"

echo "Progressing the chain by 12 blocks via alpha"
tmux select-window -t $TMUX_SESSION:1
tmux send-keys "./advance 12; tmux wait -S advance-alpha" C-m
tmux wait advance-alpha

echo "Progressing the chain by 12 blocks via beta"
tmux select-window -t $TMUX_SESSION:5
tmux send-keys "./advance 12; tmux wait -S advance-beta" C-m
tmux wait advance-beta

echo "Progressed the chain to Stake Validation Height (SVH)"
echo Attach to simnet nodes/wallets with \"tmux a -t $TMUX_SESSION\".
# tmux attach-session -t $TMUX_SESSION


# TODO: the harness currently creates coinbases, tickets, votes and regular
# transactions. It'll need to generate revocations and swap transactions as well.