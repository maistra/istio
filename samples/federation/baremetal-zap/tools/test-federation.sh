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

log "check status of federation"

log "oc1 -n mesh1-system get servicemeshpeer mesh2 -o json | jq .status:"

oc1 -n mesh1-system get servicemeshpeer mesh2 -o json | jq .status

log "oc2 -n mesh2-system get servicemeshpeer mesh1 -o json | jq .status:"
oc2 -n mesh2-system get servicemeshpeer mesh1 -o json | jq .status

log "Check if services from mesh1 are imported into mesh2:"


log "oc2 -n mesh2-system get importedservicesets mesh1 -o json | jq .status:"
oc2 -n mesh2-system get importedservicesets mesh1 -o json | jq .status

log "deploy v2 ratings system into mesh1 and mesh2"

oc1 logs -n mesh1-bookinfo deploy/ratings-v2-mysql -f &
oc2 logs -n mesh2-bookinfo deploy/ratings-v2-mysql -f &

log "manual steps to test:
  1. Open http://$(oc2 -n mesh2-system get route istio-ingressgateway -o json | jq -r .spec.host)/productpage
  2. Refresh the page several times and observe requests hitting either the mesh1 or the mesh2 cluster.
"
