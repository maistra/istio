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

# might want to script in changing the images accessed by bookinfo by platform here

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
