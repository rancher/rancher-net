#!/bin/bash
set -e -x

trap "exit 1" SIGTERM SIGINT

export PIDFILE=/var/run/rancher-net.pid
export LOCAL_IP=$(ip route get 8.8.8.8 | grep via | awk '{print $7}')
export LOCAL_IP_WITH_SUBNET=$(ip addr | grep ${LOCAL_IP} | awk '{print $2}')

if [ "${RANCHER_DEBUG}" == "true" ]; then
    DEBUG="--debug"
else
    DEBUG=""
fi

GATEWAY=$(ip route get 8.8.8.8 | awk '{print $3}')
iptables -t nat -I POSTROUTING -o vtep1042 -s $GATEWAY -j MASQUERADE
exec rancher-net \
-i ${LOCAL_IP_WITH_SUBNET} \
--pid-file ${PIDFILE} \
--use-metadata \
--backend vxlan \
${DEBUG}
