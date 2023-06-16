#!/usr/bin/env bash

# Copyright Maistra Authors. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -ex

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null && pwd )"

function image_retag() {
  if [ "$#" -ne 1 ]; then
    echo "Usage: image_retag component"
    exit 2
  fi

  local istio_component="${1}"
  local istio_src_component="${istio_component}"

  if [ "${istio_component}" = "istio-cni" ]; then
      istio_src_component="install-cni"
  fi

  "${CONTAINER_CLI}" tag "${HUB}/${istio_src_component}:${TAG}" "${HUB}/${istio_component}-${BASE_DISTRIBUTION}:${TAG}"
  "${CONTAINER_CLI}" rmi "${HUB}/${istio_src_component}:${TAG}"
}

function cleanup() {
  if [ -f "${DIR}"/../../tools/docker.yaml.upstream ]; then
    mv "${DIR}"/../../tools/docker.yaml.upstream "${DIR}"/../../tools/docker.yaml
  fi
}

## Main
if [ "$#" -ne 1 ]; then
  echo "Usage: post-build.sh component"
  exit 1
fi

image_retag "${1}"
cleanup