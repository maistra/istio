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

set +e

# shellcheck disable=SC1091
source tools/common.sh


log "Retrieving root certificates"
MESH1_CERT=$(oc1 get configmap -n mesh1-system istio-ca-root-cert -o jsonpath='{.data.root-cert\.pem}' | sed ':a;N;$!ba;s/\n/\\\n    /g')
MESH2_CERT=$(oc2 get configmap -n mesh2-system istio-ca-root-cert -o jsonpath='{.data.root-cert\.pem}' | sed ':a;N;$!ba;s/\n/\\\n    /g')

MESH1_DISCOVERY_PORT="8188"
MESH1_SERVICE_PORT="15443"
MESH2_DISCOVERY_PORT="8188"
MESH2_SERVICE_PORT="15443"

log "Retrieving ingress addresses"
if [ "${MESH1_KUBECONFIG}" == "${MESH2_KUBECONFIG}" ]; then
  echo "Single cluster detected; using cluster-local service for ingress"
  MESH1_ADDRESS=mesh2-ingress.mesh1-system.svc.cluster.local
  MESH2_ADDRESS=mesh1-ingress.mesh2-system.svc.cluster.local
  echo MESH1_ADDRESS=${MESH1_ADDRESS}
  echo MESH2_ADDRESS=${MESH2_ADDRESS}
else
  echo "Two clusters detected; using load-balancer service for ingress"
  MESH1_ADDRESS=$(oc1 -n mesh1-system get svc mesh2-ingress -o jsonpath="{.status.loadBalancer.ingress[].ip}")
  if [ -z "$MESH1_ADDRESS" ]; then
    echo "No loadBalancer.ingress.ip found in service mesh2-ingress; trying hostname"
    MESH1_ADDRESS=$(oc1 -n mesh1-system get svc mesh2-ingress -o jsonpath="{.status.loadBalancer.ingress[].hostname}")
    if [ -z "$MESH1_ADDRESS" ]; then
      echo "LoadBalancer service mesh2-ingress has no externally reachable ip or hostname; falling back to node ports"
      MESH1_ADDRESS=$(oc1 get nodes -o json | jq -r '.items[0].status.addresses[] | select (.type == "Hostname") | .address')
      MESH1_DISCOVERY_PORT=$(oc1 -n mesh1-system get svc mesh2-ingress -o json | jq '.spec.ports[] | select (.name == "https-discovery") | .nodePort')
      MESH1_SERVICE_PORT=$(oc1 -n mesh1-system get svc mesh2-ingress -o json | jq '.spec.ports[] | select (.name == "tls") | .nodePort')
      if [ -z "$MESH1_ADDRESS" ]; then
        echo "FATAL: Could not determine address for mesh2-ingress in mesh1"
        exit 1
      fi
    fi
  fi
  echo MESH1_ADDRESS="${MESH1_ADDRESS}"
  echo MESH1_DISCOVERY_PORT="${MESH1_DISCOVERY_PORT}"
  echo MESH1_SERVICE_PORT="${MESH1_SERVICE_PORT}"

  MESH2_ADDRESS=$(oc2 -n mesh2-system get svc mesh1-ingress -o jsonpath="{.status.loadBalancer.ingress[].ip}")
  if [ -z "$MESH2_ADDRESS" ]; then
    echo "No loadBalancer.ingress.ip found in service mesh1-ingress; trying hostname"
    MESH2_ADDRESS=$(oc2 -n mesh2-system get svc mesh1-ingress -o jsonpath="{.status.loadBalancer.ingress[].hostname}")
    if [ -z "$MESH2_ADDRESS" ]; then
      echo "LoadBalancer service mesh1-ingress has no externally reachable ip or hostname; falling back to node ports"
      MESH2_ADDRESS=$(oc2 get nodes -o json | jq -r '.items[0].status.addresses[] | select (.type == "Hostname") | .address')
      MESH2_DISCOVERY_PORT=$(oc2 -n mesh2-system get svc mesh1-ingress -o json | jq '.spec.ports[] | select (.name == "https-discovery") | .nodePort')
      MESH2_SERVICE_PORT=$(oc2 -n mesh2-system get svc mesh1-ingress -o json | jq '.spec.ports[] | select (.name == "tls") | .nodePort')
      if [ -z "$MESH2_ADDRESS" ]; then
        echo "FATAL: Could not determine address for mesh1-ingress in mesh2"
        exit 1
      fi
    fi
  fi
  echo MESH2_ADDRESS="${MESH2_ADDRESS}"
  echo MESH2_DISCOVERY_PORT="${MESH2_DISCOVERY_PORT}"
  echo MESH2_SERVICE_PORT="${MESH2_SERVICE_PORT}"
fi



#-----

log "Deleting mongodb k8s Service for mesh2"
oc2 delete -f import/mongodb-service.yaml

log "Deleting VirtualServices for mesh2"
oc2 delete -f examples/mongodb-remote-virtualservice.yaml
oc2 delete -f examples/ratings-split-virtualservice.yaml

log "Deleting bookinfo in mesh1"
oc1 -n mesh1-bookinfo delete -f bookinfo/platform/kube/bookinfo.yaml
oc1 -n mesh1-bookinfo apply -f bookinfo/platform/kube/bookinfo-ratings-v2-mysql.yaml
oc1 -n mesh1-bookinfo apply -f bookinfo/platform/kube/bookinfo-mysql.yaml
oc1 -n mesh1-bookinfo delete -f bookinfo/networking/destination-rule-all.yaml

log "Deleting bookinfo in mesh2"
oc2 -n mesh2-bookinfo delete -f bookinfo/platform/kube/bookinfo.yaml
oc2 -n mesh2-bookinfo apply -f bookinfo/platform/kube/bookinfo-ratings-v2-mysql.yaml
oc2 -n mesh2-bookinfo delete -f bookinfo/networking/bookinfo-gateway.yaml
oc2 -n mesh2-bookinfo delete -f bookinfo/networking/destination-rule-all.yaml
oc2 -n mesh2-bookinfo delete -f bookinfo/networking/virtual-service-reviews-v3.yaml

log "Disabling federation for mesh1"
oc1 delete -f export/exportedserviceset.yaml
sed -e "s:{{MESH2_ADDRESS}}:$MESH2_ADDRESS:g" -e "s:{{MESH2_DISCOVERY_PORT}}:$MESH2_DISCOVERY_PORT:g" -e "s:{{MESH2_SERVICE_PORT}}:$MESH2_SERVICE_PORT:g" export/servicemeshpeer.yaml.template |oc1 delete -f -
sed "s:{{MESH2_CERT}}:$MESH2_CERT:g" export/configmap.yaml.template | oc1 delete -f -

log "Disabling federation for mesh2"
oc2 delete -f import/importedserviceset.yaml
sed -e "s:{{MESH1_ADDRESS}}:$MESH1_ADDRESS:g" -e "s:{{MESH1_DISCOVERY_PORT}}:$MESH1_DISCOVERY_PORT:g" -e "s:{{MESH1_SERVICE_PORT}}:$MESH1_SERVICE_PORT:g" import/servicemeshpeer.yaml.template |oc2 delete -f -
sed "s:{{MESH1_CERT}}:$MESH1_CERT:g" import/configmap.yaml.template | oc2 delete -f -

#=====

log "Uninstalling control plane for mesh2"
oc2 delete -f import/smcp.yaml
oc2 delete -f import/smcp-bare-metal.yaml
oc2 delete -f import/smmr.yaml

log "Uninstalling control plane for mesh1"
oc1 delete -f export/smcp.yaml
oc1 delete -f export/smcp-bare-metal.yaml
oc1 delete -f export/smmr.yaml

log "Waiting for mesh1 smmr removal to complete"
oc1 wait --for condition=Ready -n mesh1-system smmr/default --timeout 300s

log "Waiting for mesh2 smmr removal to complete"
oc2 wait --for condition=Ready -n mesh2-system smmr/default --timeout 300s

log "Deleting projects for mesh1"
oc1 delete project mesh1-system 
oc1 delete project mesh1-bookinfo 

log "Deleting projects for mesh2"
oc2 delete project mesh2-system || true
oc2 delete project mesh2-bookinfo || true

log "UNINSTALLATION of ServiceMesh Federation COMPLETE"
