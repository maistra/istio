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
source tools/common.sh

log "Creating projects for mesh1"
oc1 new-project mesh1-system || true
oc1 new-project mesh1-bookinfo || true

log "Installing control plane for mesh1"
oc1 apply -f export/smcp.yaml
oc1 apply -f export/smmr.yaml

log "Creating projects for mesh2"
oc2 new-project mesh2-system || true
oc2 new-project mesh2-bookinfo || true

log "Installing control plane for mesh2"
oc2 apply -f import/smcp.yaml
oc2 apply -f import/smmr.yaml

log "Waiting for mesh1 installation to complete"
oc1 wait --for condition=Ready -n mesh1-system smmr/default --timeout 300s

log "Waiting for mesh2 installation to complete"
oc2 wait --for condition=Ready -n mesh2-system smmr/default --timeout 300s

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

log "Enabling federation for mesh1"
sed "s:{{MESH2_CERT}}:$MESH2_CERT:g" export/configmap.yaml.template | oc1 apply -f -
sed -e "s:{{MESH2_ADDRESS}}:$MESH2_ADDRESS:g" -e "s:{{MESH2_DISCOVERY_PORT}}:$MESH2_DISCOVERY_PORT:g" -e "s:{{MESH2_SERVICE_PORT}}:$MESH2_SERVICE_PORT:g" export/servicemeshpeer.yaml.template |oc1 apply -f -
oc1 apply -f export/exportedserviceset.yaml

log "Enabling federation for mesh2"
sed "s:{{MESH1_CERT}}:$MESH1_CERT:g" import/configmap.yaml.template | oc2 apply -f -
sed -e "s:{{MESH1_ADDRESS}}:$MESH1_ADDRESS:g" -e "s:{{MESH1_DISCOVERY_PORT}}:$MESH1_DISCOVERY_PORT:g" -e "s:{{MESH1_SERVICE_PORT}}:$MESH1_SERVICE_PORT:g" import/servicemeshpeer.yaml.template |oc2 apply -f -
oc2 apply -f import/importedserviceset.yaml

log "Installing bookinfo in mesh1"
oc1 -n mesh1-bookinfo apply -f ../../../../../samples/bookinfo/platform/kube/bookinfo.yaml
oc1 -n mesh1-bookinfo apply -f ../../../../../samples/bookinfo/platform/kube/bookinfo-ratings-v2.yaml
oc1 -n mesh1-bookinfo apply -f ../../../../../samples/bookinfo/platform/kube/bookinfo-db.yaml
oc1 -n mesh1-bookinfo apply -f ../../../../../samples/bookinfo/networking/destination-rule-all.yaml

log "Installing bookinfo in mesh2"
oc2 -n mesh2-bookinfo apply -f ../../../../../samples/bookinfo/platform/kube/bookinfo.yaml
oc2 -n mesh2-bookinfo apply -f ../../../../../samples/bookinfo/platform/kube/bookinfo-ratings-v2.yaml
oc2 -n mesh2-bookinfo apply -f ../../../../../samples/bookinfo/networking/bookinfo-gateway.yaml
oc2 -n mesh2-bookinfo apply -f ../../../../../samples/bookinfo/networking/destination-rule-all.yaml
oc2 -n mesh2-bookinfo apply -f ../../../../../samples/bookinfo/networking/virtual-service-reviews-v3.yaml

log "Installing mongodb k8s Service for mesh2"
oc2 apply -f import/mongodb-service.yaml

log "Installing VirtualServices for mesh2"
oc2 apply -f examples/mongodb-remote-virtualservice.yaml
oc2 apply -f examples/ratings-split-virtualservice.yaml

log "INSTALLATION COMPLETE

Two service mesh control planes and two bookinfo applications are now installed.
The first cluster (mesh1) contains the namespace mesh1-system and mesh1-bookinfo.
The second cluster (mesh2) contains mesh2-system and mesh2-bookinfo.
Mesh1 exports services, mesh2 imports them.

The meshes are configured to split ratings traffic in mesh2-bookinfo between
mesh1 and mesh2. The ratings-v2 service in mesh2 is configured to use the
mongodb service in mesh1.

Run the following command in the mesh1 cluster to check the connection status:

  oc -n mesh1-system get servicemeshpeer mesh2 -o json | jq .status

Run the following command to check the connection status in mesh2:

  oc -n mesh2-system get servicemeshpeer mesh1 -o json | jq .status

Check if services from mesh1 are imported into mesh2:

  oc -n mesh2-system get importedservicesets mesh1 -o json | jq .status

To see federation in action, use the bookinfo app in mesh2. For example:

  1. Run this command in the mesh1 cluster: oc logs -n mesh1-bookinfo deploy/ratings-v2 -f
  2. Run this command in the mesh2 cluster: oc logs -n mesh2-bookinfo deploy/ratings-v2 -f
  3. Open http://$(oc2 -n mesh2-system get route istio-ingressgateway -o json | jq -r .spec.host)/productpage
  4. Refresh the page several times and observe requests hitting either the mesh1 or the mesh2 cluster.
"
