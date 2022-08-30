#!/usr/bin/env bash

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