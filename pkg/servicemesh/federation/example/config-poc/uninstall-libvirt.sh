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
source common.sh

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
  MESH1_DISCOVERY_PORT=$(oc1 -n mesh1-system get svc mesh2-ingress -o jsonpath="{.spec.ports[1].nodePort}")
  MESH1_SERVICE_PORT=$(oc1 -n mesh1-system get svc mesh2-ingress -o jsonpath="{.spec.ports[0].nodePort}")

  MESH2_DISCOVERY_PORT=$(oc2 -n mesh2-system get svc mesh1-ingress -o jsonpath="{.spec.ports[1].nodePort}")
  MESH2_SERVICE_PORT=$(oc2 -n mesh2-system get svc mesh1-ingress -o jsonpath="{.spec.ports[0].nodePort}")

  MESH1_HOSTNAME=$(oc1 -n mesh1-system get route mesh2-ingress -o jsonpath="{.spec.host}")
  MESH2_HOSTNAME=$(oc2 -n mesh2-system get route mesh1-ingress -o jsonpath="{.spec.host}")

  MESH1_ADDRESS=$(host $MESH1_HOSTNAME | cut -d' ' -f 4)
  MESH2_ADDRESS=$(host $MESH2_HOSTNAME | cut -d' ' -f 4)

  echo MESH1_DISCOVERY_PORT=${MESH1_DISCOVERY_PORT}
  echo MESH1_SERVICE_PORT=${MESH1_SERVICE_PORT}
  
  echo MESH2_DISCOVERY_PORT=${MESH2_DISCOVERY_PORT}
  echo MESH2_SERVICE_PORT=${MESH2_SERVICE_PORT}

  echo MESH1_HOSTNAME=${MESH1_HOSTNAME}
  echo MESH2_HOSTNAME=${MESH2_HOSTNAME}

fi



#-----

log "Deleting mongodb k8s Service for mesh2"
oc2 delete -f import/mongodb-service.yaml

log "Deleting VirtualServices for mesh2"
oc2 delete -f examples/mongodb-remote-virtualservice.yaml
oc2 delete -f examples/ratings-split-virtualservice.yaml

log "Deleting bookinfo in mesh1"
oc1 -n mesh1-bookinfo delete -f ../../../../../samples/bookinfo/platform/kube/bookinfo.yaml
oc1 -n mesh1-bookinfo delete -f ../../../../../samples/bookinfo/platform/kube/bookinfo-ratings-v2.yaml
oc1 -n mesh1-bookinfo delete -f ../../../../../samples/bookinfo/platform/kube/bookinfo-db.yaml
oc1 -n mesh1-bookinfo delete -f ../../../../../samples/bookinfo/networking/destination-rule-all.yaml

log "Deleting bookinfo in mesh2"
oc2 -n mesh2-bookinfo delete -f ../../../../../samples/bookinfo/platform/kube/bookinfo.yaml
oc2 -n mesh2-bookinfo delete -f ../../../../../samples/bookinfo/platform/kube/bookinfo-ratings-v2.yaml
oc2 -n mesh2-bookinfo delete -f ../../../../../samples/bookinfo/networking/bookinfo-gateway.yaml
oc2 -n mesh2-bookinfo delete -f ../../../../../samples/bookinfo/networking/destination-rule-all.yaml
oc2 -n mesh2-bookinfo delete -f ../../../../../samples/bookinfo/networking/virtual-service-reviews-v3.yaml

log "Disabling federation for mesh1"
oc1 delete -f export/exportedserviceset.yaml
sed -e "s:{{MESH2_ADDRESS}}:$MESH2_ADDRESS:g" -e "s:{{MESH2_DISCOVERY_PORT}}:$MESH2_DISCOVERY_PORT:g" -e "s:{{MESH2_SERVICE_PORT}}:$MESH2_SERVICE_PORT:g" export/servicemeshpeer.yaml | oc1 delete -f -
sed "s:{{MESH2_CERT}}:$MESH2_CERT:g" export/configmap.yaml | oc1 delete -f -

log "Disabling federation for mesh2"
oc2 delete -f import/importedserviceset.yaml
sed -e "s:{{MESH1_ADDRESS}}:$MESH1_ADDRESS:g" -e "s:{{MESH1_DISCOVERY_PORT}}:$MESH1_DISCOVERY_PORT:g" -e "s:{{MESH1_SERVICE_PORT}}:$MESH1_SERVICE_PORT:g" import/servicemeshpeer.yaml | oc2 delete -f -
sed "s:{{MESH1_CERT}}:$MESH1_CERT:g" import/configmap.yaml | oc2 delete -f -

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
exit

"
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
