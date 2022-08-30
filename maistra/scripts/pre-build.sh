#!/usr/bin/env bash

set -ex

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null && pwd )"

function backup() {
  mv "${DIR}"/../../tools/docker.yaml "${DIR}"/../../tools/docker.yaml.upstream && cp "${DIR}"/../docker.yaml "${DIR}"/../../tools/docker.yaml
}

## Main
backup