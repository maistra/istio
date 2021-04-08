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
# oc patch -n mesh1-system smcp/fed-export --type merge -p '{"spec":{"gateways":{"additionalIngress":{"mesh1-ingress":{"enabled":true}}}}}'
oc create -f export/meshfederation.yaml

echo "Waiting for mesh2 installation to complete"
oc wait --for condition=Ready -n mesh2-system smmr/default --timeout 180s

echo "Enabling federation mesh2"
# oc patch -n mesh2-system smcp/fed-import --type merge -p '{"spec":{"gateways":{"additionalEgress":{"mesh2-egress":{"enabled":true}}}}}'
oc create -f import/meshfederation.yaml

echo "Installing istio configuration for mesh1"
oc create -f export/http/passthrough
oc create -f export/http/aliased
# oc create -f export/http/destinationrule.yaml
oc create -f export/tcp/passthrough
oc create -f export/tcp/aliased
# oc create -f export/tcp/destinationrule.yaml

echo "Installing istio configuration for mesh2"
oc create -f import/http/passthrough
oc create -f import/http/proxied
oc create -f import/tcp/passthrough
oc create -f import/tcp/proxied
oc create -f import/destinationrule-gateway.yaml
oc create -f import/mongodb-service.yaml

echo "Please install bookinfo into mesh1-bookinfo and mesh2-bookinfo"
echo "After installing, add a maistra.io/exportAs label to the ratings service"
echo "The following label illustrates a passthrough binding (direct access to the service from the caller)"
echo "    oc label -n mesh1-bookinfo service/ratings --overwrite maistra.io/exportAs=ratings.mesh1-bookinfo.svc.cluster.local"
echo "The following label illustrates an aliased binding (caller is terminated at ingress gateway)"
echo "    oc label -n mesh1-bookinfo service/ratings --overwrite maistra.io/exportAs=ratings-aliased.mesh1-exports.svc.cluster.local"
echo "For the tcp example, install bookinfo-db into mesh1-bookinfo and"
echo "bookinfo-ratings-v2 into mesh2-bookinfo, for example:"
echo "    oc create -n mesh1-bookinfo bookinfo-db.yaml"
echo "    oc create -n mesh2-bookinfo bookinfo-ratings-v2.yaml"
echo "After installing, add a maistra.io/exportAs label to the mongodb service"
echo "The following label illustrates a passthrough binding (direct access to the service from the caller)"
echo "    oc label -n mesh1-bookinfo service/mongodb --overwrite maistra.io/exportAs=mongodb.mesh1-bookinfo.svc.cluster.local"
echo "The following label illustrates an aliased binding (caller is terminated at ingress gateway)"
echo "    oc label -n mesh1-bookinfo service/mongodb --overwrite maistra.io/exportAs=mongodb-aliased.mesh1-exports.svc.cluster.local"
