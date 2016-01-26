#!/bin/bash
set -e

source ${CATTLE_HOME:-/var/lib/cattle}/common/scripts.sh

cd $(dirname $0)

chmod +x bin/rancher-net

mkdir -p content-home
mv bin content-home

STRONGSWAN=$(echo strongswan/*)
STAMP=/usr/local/.$(basename $STRONGSWAN)

if [[ -e "$STRONGSWAN" && ! -e $STAMP ]]; then
    echo Extracting $STRONGSWAN
    tar xf $STRONGSWAN -C /
    touch $STAMP
fi

# Make sure that when node start is doesn't think it holds the config.sh lock
unset CATTLE_CONFIG_FLOCKER

if [ "$CATTLE_AGENT_STARTUP" != "true" ]; then
    killall -9 rancher-net || true
    killall -HUP charon || true
    if ! $CATTLE_HOME/bin/rancher-net --test-charon; then
        # If we can't talk to charon, restart it
        killall -9 charon || true
    fi
fi

stage_files
