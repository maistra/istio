#!/bin/bash

# Copyright Red Hat, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http:#www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e

# shellcheck disable=SC1091
source common.sh

log "Retrieving root certificates"
MESH1_CERT=$(oc1 get configmap -n mesh1-system istio-ca-root-cert -o jsonpath='{.data.root-cert\.pem}' | sed ':a;N;$!ba;s/\n/\\\n    /g')
MESH2_CERT=$(oc2 get configmap -n mesh2-system istio-ca-root-cert -o jsonpath='{.data.root-cert\.pem}' | sed ':a;N;$!ba;s/\n/\\\n    /g')

MESH1_DISCOVERY_PORT="8188"
MESH1_SERVICE_PORT="15443"
MESH2_DISCOVERY_PORT="8188"
MESH2_SERVICE_PORT="15443"

log "Retrieving ingress addresses"

log "Retrieving ingress addresses"
if [ "${MESH1_KUBECONFIG}" == "${MESH2_KUBECONFIG}" ]; then
  echo "Single cluster detected; using cluster-local service for ingress"
  MESH1_ADDRESS=mesh2-ingress.mesh1-system.svc.cluster.local
  MESH2_ADDRESS=mesh1-ingress.mesh2-system.svc.cluster.local
  echo MESH1_ADDRESS=${MESH1_ADDRESS}
  echo MESH2_ADDRESS=${MESH2_ADDRESS}

else
  log "Two clusters detected; using node port services for ingress"

  MESH1_DISCOVERY_PORT=$(oc1 -n mesh1-system get svc mesh2-ingress -o jsonpath="{.spec.ports[1].nodePort}")
  MESH1_SERVICE_PORT=$(oc1 -n mesh1-system get svc mesh2-ingress -o jsonpath="{.spec.ports[0].nodePort}")

  MESH2_DISCOVERY_PORT=$(oc2 -n mesh2-system get svc mesh1-ingress -o jsonpath="{.spec.ports[1].nodePort}")
  MESH2_SERVICE_PORT=$(oc2 -n mesh2-system get svc mesh1-ingress -o jsonpath="{.spec.ports[0].nodePort}")

  MESH1_HOSTNAME=$(oc1 -n mesh1-system get route mesh2-ingress -o jsonpath="{.spec.host}")
  MESH2_HOSTNAME=$(oc2 -n mesh2-system get route mesh1-ingress -o jsonpath="{.spec.host}")


  echo MESH1_DISCOVERY_PORT=${MESH1_DISCOVERY_PORT}
  echo MESH1_SERVICE_PORT=${MESH1_SERVICE_PORT}
  
  echo MESH2_DISCOVERY_PORT=${MESH2_DISCOVERY_PORT}
  echo MESH2_SERVICE_PORT=${MESH2_SERVICE_PORT}

  echo MESH1_HOSTNAME=${MESH1_HOSTNAME}
  echo MESH2_HOSTNAME=${MESH2_HOSTNAME}

  if [ -z "$MESH1_HOSTNAME" ]; then
    echo "No route address found for service mesh2-ingress in mesh1; exit"
    exit
  else
    MESH1_ADDRESS=$(host $MESH1_HOSTNAME | cut -d' ' -f 4)
    log $MESH1_HOSTNAME has address $MESH1_ADDRESS

    if [ ${MESH1_ADDRESS} == "found" ]; then
      echo "mesh1 mesh2-ingress service address unresolvable; check dns settings; exit"
      exit
    else
      log "checking if service port $MESH1_SERVICE_PORT is open on $MESH1_ADDRESS"

      nc -z  ${MESH1_ADDRESS} ${MESH1_SERVICE_PORT}
      connect=$?

      echo "exit code of ncat $MESH1_ADDRESS $MESH1_SERVICE_PORT is $connect"

      if [ $connect -ne 0 ]; then 
         log "temporarily opening firewall port for $MESH1_ADDRESS $MESH1_SERVICE_PORT"

         # this is particular to libvirt multi-cluster installations on a single host
         # will need a more elaborate script for multiple clusters on different hosts
         # we don't make it a permanent addition because this script is only for test

         firewall-cmd --add-port=${MESH1_SERVICE_PORT}/tcp --zone=libvirt
         firewall-cmd --add-port=${MESH1_SERVICE_PORT}/ucp --zone=libvirt
      fi

      log "checking if discovery port $MESH1_DISCOVERY_PORT is open on $MESH1_ADDRESS"
      nc -zw 1 ${MESH1_ADDRESS} ${MESH1_DISCOVERY_PORT}
      connect=$?
      if [ $connect -ne 0 ]; then
         echo temporarily opening firewall port for $MESH1_ADDRESS $MESH1_DISCOVERY_PORT

         # QUESTION: should we be opening the port for the whole zone or should it 
         #           be particular to the virtual network that the cluster is on?

         firewall-cmd --add-port=${MESH1_DISCOVERY_PORT}/tcp --zone=libvirt
         firewall-cmd --add-port=${MESH1_DISCOVERY_PORT}/udp --zone=libvirt
      fi
  fi

  log "exiting to debug"
fi
exit

  if [ -z "$MESH2_HOSTNAME" ]; then
    log "No route address found for service mesh1-ingress in mesh2; exit"
    exit
  else
    MESH2_ADDRESS=$(host ${MESH2_HOSTNAME} | cut -d' ' -f 4)
    if [ ${MESH1_ADDRESS} == "found" ]; then
      log "mesh2 mesh1-ingress service address unresolvable; check dns settings; exit"
      exit
    else
      ncat -z $MESH2_ADDRESS $MESH2_SERVICE_PORT
      connect=$?
      if [ $connect -ne 0 ]; then 
         echo temporarily opening firewall port for $MESH2_ADDRESS $MESH2_SERVICE_PORT
         firewall-cmd --add-port=$MESH2_SERVICE_PORT/tcp --zone=libvirt
         firewall-cmd --add-port=$MESH2_SERVICE_PORT/ucp --zone=libvirt
      fi
      ncat -z $MESH2_ADDRESS $MESH2_DISCOVERY_PORT
      connect=$?
      if [ $connect -ne 0 ]; then
         echo temporarily opening firewall port for $MESH2_ADDRESS $MESH2_DISCOVERY_PORT

         firewall-cmd --add-port=${MESH2_DISCOVERY_PORT}/tcp --zone=libvirt
         firewall-cmd --add-port=${MESH2_DISCOVERY_PORT}/udp --zone=libvirt
      fi
    fi
  fi

fi

log "Enabling federation for mesh1"

sed "s:{{MESH2_CERT}}:$MESH2_CERT:g" export/configmap.yaml | oc1 apply -f -
sed -e "s:{{MESH2_ADDRESS}}:$MESH2_ADDRESS:g" -e "s:{{MESH2_DISCOVERY_PORT}}:$MESH2_DISCOVERY_PORT:g" -e "s:{{MESH2_SERVICE_PORT}}:$MESH2_SERVICE_PORT:g" export/servicemeshpeer.yaml | oc1 apply -f -

oc1 apply -f export/exportedserviceset.yaml

log "Enabling federation for mesh2"

sed "s:{{MESH1_CERT}}:$MESH1_CERT:g" import/configmap.yaml | oc2 apply -f -

sed -e "s:{{MESH1_ADDRESS}}:$MESH1_ADDRESS:g" -e "s:{{MESH1_DISCOVERY_PORT}}:$MESH1_DISCOVERY_PORT:g" -e "s:{{MESH1_SERVICE_PORT}}:$MESH1_SERVICE_PORT:g" import/servicemeshpeer.yaml | oc2 apply -f -

oc2 apply -f import/importedserviceset.yaml


