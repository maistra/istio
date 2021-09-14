#!/bin/bash

# Copyright 2018 Istio Authors
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

# Init script downloads or updates envoy and the go dependencies. Called from Makefile, which sets
# the needed environment variables.


set -o errexit
set -o nounset
set -o pipefail

export GO_TOP=${GO_TOP:-$(echo "${GOPATH}" | cut -d ':' -f1)}
export OUT_DIR=${OUT_DIR:-${GO_TOP}/out}

HELM_VER=${HELM_VER:-v2.10.0}

export GOPATH=${GOPATH:-$GO_TOP}
# Normally set by Makefile
export ISTIO_BIN=${ISTIO_BIN:-${GOPATH}/bin}

# Set the architecture. Matches logic in the Makefile.
export GOARCH=${GOARCH:-'amd64'}

# Determine the OS. Matches logic in the Makefile.
LOCAL_OS=${OSTYPE}
case $LOCAL_OS in
  "linux"*)
    LOCAL_OS='linux'
    ;;
  "darwin"*)
    LOCAL_OS='darwin'
    ;;
  *)
    echo "This system's OS ${LOCAL_OS} isn't recognized/supported"
    exit 1
    ;;
esac
export GOOS=${GOOS:-${LOCAL_OS}}

# Gets the download command supported by the system (currently either curl or wget)
# simplify because init.sh has already decided which one is
DOWNLOAD_COMMAND=""
function set_download_command () {
    # Try curl.
    if command -v curl > /dev/null; then
        if curl --version | grep Protocols  | grep https > /dev/null; then
	       DOWNLOAD_COMMAND='curl -Lo '
	       return
        fi
    fi

    # Try wget.
    if command -v wget > /dev/null; then
        DOWNLOAD_COMMAND='wget -O '
        return
    fi
}
set_download_command

# test scripts seem to like to run this script directly rather than use make
export ISTIO_OUT=${ISTIO_OUT:-${ISTIO_BIN}}

# install helm if not present, it must be the local version.
if [ ! -f "${ISTIO_OUT}/version.helm.${HELM_VER}" ] ; then
    TD=$(mktemp -d)
    # Install helm. Please keep it in sync with .circleci
    cd "${TD}" && \
        ${DOWNLOAD_COMMAND} "${TD}/helm.tgz" "https://get.helm.sh/helm-${HELM_VER}-${LOCAL_OS}-amd64.tar.gz" && \
        tar xfz helm.tgz && \
        mv ${LOCAL_OS}-amd64/helm "${ISTIO_OUT}/helm-${HELM_VER}" && \
        cp "${ISTIO_OUT}/helm-${HELM_VER}" "${ISTIO_OUT}/helm" && \
        rm -rf "${TD}" && \
        touch "${ISTIO_OUT}/version.helm.${HELM_VER}"
fi
