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

echo "Creating projects for mesh1"
oc new-project mesh1-system || true
oc new-project mesh1-bookinfo || true

echo "Creating CA Secret for mesh1"
oc create secret generic cacerts -n mesh1-system --from-file ./cacerts/

echo "Installing control plane for mesh1"
oc create -f export/smcp.yaml
oc create -f export/smmr.yaml

echo "Creating projects for mesh2"
oc new-project mesh2-system || true
oc new-project mesh2-bookinfo || true

echo "Creating CA Secret for mesh2"
oc create secret generic cacerts -n mesh2-system --from-file ./cacerts/

echo "Installing control plane for mesh2"
oc create -f import/smcp.yaml
oc create -f import/smmr.yaml

echo "Waiting for mesh1 installation to complete"
oc wait --for condition=Ready -n mesh1-system smmr/default --timeout 180s

echo "Enabling federation for mesh1"
oc create -f export/meshfederation.yaml
oc create -f export/serviceexports.yaml

echo "Waiting for mesh2 installation to complete"
oc wait --for condition=Ready -n mesh2-system smmr/default --timeout 180s

echo "Enabling federation mesh2"
oc create -f import/meshfederation.yaml
oc create -f import/serviceimports.yaml

echo "Installing mongodb k8s Service for mesh2"
oc create -f import/mongodb-service.yaml

echo "Installing VirtualServices for mesh2"
oc create -f examples/mongodb-remote-virtualservice.yaml
oc create -f examples/ratings-split-virtualservice.yaml

echo "Please install bookinfo into mesh1-bookinfo and mesh2-bookinfo"
echo "For the tcp example, install bookinfo-db into mesh1-bookinfo and"
echo "bookinfo-ratings-v2 into mesh2-bookinfo, for example:"
echo "    oc create -n mesh1-bookinfo bookinfo-db.yaml"
echo "    oc create -n mesh2-bookinfo bookinfo-ratings-v2.yaml"
echo ""
echo "The meshes are configured to split ratings traffic in mesh2-bookinfo"
echo "between mesh1 and mesh2.  The ratings-v2 service in mesh2 is configured to"
echo "use the mongodb service in mesh1."
