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
oc1 apply -f export/smcp-bare-metal.yaml
oc1 apply -f export/smmr.yaml

log "Creating projects for mesh2"
oc2 new-project mesh2-system || true
oc2 new-project mesh2-bookinfo || true

log "Installing control plane for mesh2"
oc2 apply -f import/smcp-bare-metal.yaml
oc2 apply -f import/smmr.yaml

log "Waiting for mesh1 installation to complete"
oc1 wait --for condition=Ready -n mesh1-system smmr/default --timeout 300s

log "Waiting for mesh2 installation to complete"
oc2 wait --for condition=Ready -n mesh2-system smmr/default --timeout 300s


log "Retrieving root certificates"

MESH1_CERT=$(oc1 get configmap -n mesh1-system \
             istio-ca-root-cert -o jsonpath='{.data.root-cert\.pem}' | \
             sed ':a;N;$!ba;s/\n/\\\n    /g')

MESH2_CERT=$(oc2 get configmap -n mesh2-system \
             istio-ca-root-cert -o jsonpath='{.data.root-cert\.pem}' | \
             sed ':a;N;$!ba;s/\n/\\\n    /g')

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

  # we're proxying the traffic by port on the hypervisor so send all traffic there
  # MESH1_ADDRESS=$(hostname -I | cut -d' ' -f 1)
  # MESH2_ADDRESS=$(hostname -I | cut -d' ' -f 1)

  MESH1_ADDRESS=$PROXY_ADDRESS
  MESH2_ADDRESS=$PROXY_ADDRESS

  echo MESH1_DISCOVERY_PORT="${MESH1_DISCOVERY_PORT}"
  echo MESH1_SERVICE_PORT="${MESH1_SERVICE_PORT}"
  
  echo MESH2_DISCOVERY_PORT="${MESH2_DISCOVERY_PORT}"
  echo MESH2_SERVICE_PORT="${MESH2_SERVICE_PORT}"

  echo MESH1_HOSTNAME="${MESH1_HOSTNAME}"
  echo MESH2_HOSTNAME="${MESH2_HOSTNAME}"

  log "temporarily opened firewall ports may be closed later with firewall-cmd --reload"
  echo "opening firewall port for $MESH1_ADDRESS $MESH1_SERVICE_PORT"

  firewall-cmd --add-port="${MESH1_SERVICE_PORT}"/tcp --zone=libvirt
  firewall-cmd --add-port="${MESH1_SERVICE_PORT}"/udp --zone=libvirt

  firewall-cmd --add-port="${MESH1_DISCOVERY_PORT}"/tcp --zone=libvirt
  firewall-cmd --add-port="${MESH1_DISCOVERY_PORT}"/udp --zone=libvirt

  firewall-cmd --add-port="${MESH2_SERVICE_PORT}"/tcp --zone=libvirt
  firewall-cmd --add-port="${MESH2_SERVICE_PORT}"/udp --zone=libvirt

  firewall-cmd --add-port="${MESH2_DISCOVERY_PORT}"/tcp --zone=libvirt
  firewall-cmd --add-port="${MESH2_DISCOVERY_PORT}"/udp --zone=libvirt

fi

log "Generating a proxy configuration for mesh1 & mesh2 to pass packets to each other on the specified ports"

sed -e "s:{{MESH1_DISCOVERY_PORT}}:$MESH1_DISCOVERY_PORT:g" \
    -e "s:{{MESH1_SERVICE_PORT}}:$MESH1_SERVICE_PORT:g"     \
    -e "s:{{MESH2_DISCOVERY_PORT}}:$MESH2_DISCOVERY_PORT:g" \
    -e "s:{{MESH2_SERVICE_PORT}}:$MESH2_SERVICE_PORT:g"     \
    haproxy/federation.cfg.template > federation.cfg

echo "
backend mesh1-service
    mode tcp
    balance source " >> federation.cfg
for NodeName in $(oc1 get nodes -o wide -o jsonpath="{.items[*].status.addresses[1].address}")
do
  NodeIP=$(oc1 get node "${NodeName}" -o wide -o jsonpath="{.status.addresses[0].address}")
  echo "    server      $NodeName ${NodeIP}:${MESH1_SERVICE_PORT} check" >> federation.cfg
done

echo "
backend mesh1-discovery
    mode tcp
    balance source " >> federation.cfg

for NodeName in $(oc1 get nodes -o wide -o jsonpath="{.items[*].status.addresses[1].address}")
do
  NodeIP=$(oc1 get node "${NodeName}" -o wide -o jsonpath="{.status.addresses[0].address}")
  echo "    server      $NodeName ${NodeIP}:${MESH1_DISCOVERY_PORT} check" >> federation.cfg
done

echo "
backend mesh2-service
    mode tcp
    balance source " >> federation.cfg

for NodeName in $(oc2 get nodes -o wide -o jsonpath="{.items[*].status.addresses[1].address}")
do
  NodeIP=$(oc2 get node "${NodeName}" -o wide -o jsonpath="{.status.addresses[0].address}")
  echo "    server      $NodeName ${NodeIP}:${MESH2_SERVICE_PORT} check" >> federation.cfg
done

echo "
backend mesh2-discovery
    mode tcp
    balance source " >> federation.cfg

for NodeName in $(oc2 get nodes -o wide -o jsonpath="{.items[*].status.addresses[1].address}")
do
  NodeIP=$(oc2 get node "${NodeName}" -o wide -o jsonpath="{.status.addresses[0].address}")
  echo "    server      $NodeName ${NodeIP}:${MESH2_DISCOVERY_PORT} check" >> federation.cfg
done

log "add '-f federation.cfg' to /etc/systemctl/haproxy OPTION variable and restart haproxy..."

cp federation.cfg /etc/haproxy
systemctl daemon-reload
systemctl restart haproxy

log "Enabling federation for mesh1"

sed "s:{{MESH2_CERT}}:$MESH2_CERT:g" export/configmap.yaml.template | oc1 apply -f -
sed -e "s:{{MESH2_ADDRESS}}:$MESH2_ADDRESS:g" \
    -e "s:{{MESH2_DISCOVERY_PORT}}:$MESH2_DISCOVERY_PORT:g" \
    -e "s:{{MESH2_SERVICE_PORT}}:$MESH2_SERVICE_PORT:g" \
  export/servicemeshpeer.yaml.template |oc1 apply -f -

oc1 apply -f export/exportedserviceset.yaml

log "Enabling federation for mesh2"

sed "s:{{MESH1_CERT}}:$MESH1_CERT:g" import/configmap.yaml.template | oc2 apply -f -

sed -e "s:{{MESH1_ADDRESS}}:$MESH1_ADDRESS:g" \
    -e "s:{{MESH1_DISCOVERY_PORT}}:$MESH1_DISCOVERY_PORT:g" \
    -e "s:{{MESH1_SERVICE_PORT}}:$MESH1_SERVICE_PORT:g" \
  import/servicemeshpeer.yaml.template |oc2 apply -f -

oc2 apply -f import/importedserviceset.yaml


log "Installing bookinfo in mesh1"
oc1 -n mesh1-bookinfo apply -f bookinfo/platform/kube/bookinfo.yaml
oc1 -n mesh1-bookinfo apply -f bookinfo/platform/kube/bookinfo-ratings-v2-mysql.yaml
oc1 -n mesh1-bookinfo apply -f bookinfo/platform/kube/bookinfo-mysql.yaml
oc1 -n mesh1-bookinfo apply -f bookinfo/platform/kube/bookinfo-db.yaml
oc1 -n mesh1-bookinfo apply -f bookinfo/networking/destination-rule-all.yaml

log "Installing bookinfo in mesh2"
oc2 -n mesh2-bookinfo apply -f bookinfo/platform/kube/bookinfo.yaml
oc2 -n mesh2-bookinfo apply -f bookinfo/platform/kube/bookinfo-ratings-v2-mysql.yaml
oc2 -n mesh2-bookinfo apply -f bookinfo/networking/bookinfo-gateway.yaml
oc2 -n mesh2-bookinfo apply -f bookinfo/networking/destination-rule-all.yaml
oc2 -n mesh2-bookinfo apply -f bookinfo/networking/virtual-service-reviews-v3.yaml

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

  1. Run this command in the mesh1 cluster: oc logs -n mesh1-bookinfo deploy/ratings-v2-mysql -f
  2. Run this command in the mesh2 cluster: oc logs -n mesh2-bookinfo deploy/ratings-v2-mysql -f
  3. Open http://$(oc2 -n mesh2-system get route istio-ingressgateway -o json | jq -r .spec.host)/productpage
  4. Refresh the page several times and observe requests hitting either the mesh1 or the mesh2 cluster.

To just do a quick test, run ./tools/test-federation.sh
"
