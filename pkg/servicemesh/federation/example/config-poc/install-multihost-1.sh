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

log "Creating projects for mesh1"
oc1 new-project mesh1-system || true
oc1 new-project mesh1-bookinfo || true

log "Installing control plane for mesh1"
oc1 apply -f export/smcp-multihost.yaml
oc1 apply -f export/smmr.yaml

log "Creating projects for mesh2"
oc2 new-project mesh2-system || true
oc2 new-project mesh2-bookinfo || true

log "Installing control plane for mesh2"
oc2 apply -f import/smcp-multihost.yaml
oc2 apply -f import/smmr.yaml

log "Waiting for mesh1 installation to complete"
oc1 wait --for condition=Ready -n mesh1-system smmr/default --timeout 300s

log "Waiting for mesh2 installation to complete"
oc2 wait --for condition=Ready -n mesh2-system smmr/default --timeout 300s

echo "Ready to run script install-multihost-2.sh"
