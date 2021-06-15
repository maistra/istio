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

if [ ! -f "${MESH1_KUBECONFIG}" ] || [ ! -f "${MESH2_KUBECONFIG}" ]; then
  echo "Environment variables MESH1_KUBECONFIG and/or MESH2_KUBECONFIG aren't set."
  echo "Please point each to the kubeconfig file of the cluster you want to deploy"
  echo "the federated service meshes to. The first cluster exports services;"
  echo "the second imports them."
  echo
  echo "NOTE: You can use a single cluster by pointing both environment variables"
  echo "to the same file. The meshes and apps are always deployed in different"
  echo "namespaces (mesh1-system, mesh1-bookinfo, mesh2-system, mesh2-bookinfo),"
  echo "so there are no conflicts."
  exit 1
fi

oc1() {
  if [ -f "${MESH1_KUBECONFIG}" ]; then
    oc --kubeconfig="${MESH1_KUBECONFIG}" "$@"
  else
    oc "$@"
  fi
}

oc2() {
  if [ -f "${MESH2_KUBECONFIG}" ]; then
    oc --kubeconfig="${MESH2_KUBECONFIG}" "$@"
  else
    oc "$@"
  fi
}

log() {
  echo
  echo "##### $*"
}

log "Using the following kubeconfig files:
mesh1: ${MESH1_KUBECONFIG}
mesh2: ${MESH2_KUBECONFIG}"

