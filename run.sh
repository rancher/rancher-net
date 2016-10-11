#!/bin/bash

if [ ! -z ${RANCHER_DEBUG} ]; then
    set -x
fi

/opt/rancher/bin/rancher-net --debug --use-metadata --backend vxlan
