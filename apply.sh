#!/bin/bash

source ${CATTLE_HOME:-/var/lib/cattle}/common/scripts.sh

cd $(dirname $0)

chmod +x bin/rancher-net

mkdir -p content-home
mv bin content-home

STRONGSWAN=$(echo strongswan/*)
STAMP=/usr/local/.$(basename $STRONGSWAN)

if [ ! -e $STAMP ]; then
    tar xvf $STRONGSWAN -C /
    touch $STAMP
fi

stage_files

# Make sure that when node start is doesn't think it holds the config.sh lock
unset CATTLE_CONFIG_FLOCKER

if /etc/init.d/rancher-net status; then
    /etc/init.d/rancher-net restart
else
    /etc/init.d/rancher-net start
fi
