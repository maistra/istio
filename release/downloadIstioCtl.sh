#!/bin/sh

# Copyright Istio Authors
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
##############################################################################

# Separate downloader for istioctl
#
# You can fetch the istioctl file using:
# curl -sL https://raw.githubusercontent.com/istio/istio/${BRANCH}/release/downloadIstioCtl.sh | sh -
#
# where ${BRANCH} is either your branch name (e.g. release-1.4) or master.
#

# Determines the operating system.
OS="${TARGET_OS:-$(uname)}"
if [ "${OS}" = "Darwin" ] ; then
  OSEXT="osx"
else
  OSEXT="linux"
fi

# Determine the latest Istio version by version number ignoring alpha, beta, and rc versions.
if [ "${ISTIO_VERSION}" = "" ] ; then
  ISTIO_VERSION="$(curl -sL https://github.com/istio/istio/releases | \
                  grep -o 'releases/[0-9]*.[0-9]*.[0-9]*/' | sort -V | \
                  tail -1 | awk -F'/' '{ print $2}')"
  ISTIO_VERSION="${ISTIO_VERSION##*/}"
fi

if [ "${ISTIO_VERSION}" = "" ] ; then
  printf "Unable to get latest Istio version. Set ISTIO_VERSION env var and re-run. For example: export ISTIO_VERSION=1.14.0"
  exit 1;
fi

LOCAL_ARCH=$(uname -m)
if [ "${TARGET_ARCH}" ]; then
    LOCAL_ARCH=${TARGET_ARCH}
fi

case "${LOCAL_ARCH}" in
  x86_64|amd64)
    ISTIO_ARCH=amd64
    ;;
  armv8*|aarch64*|arm64)
    ISTIO_ARCH=arm64
    ;;
  armv*)
    ISTIO_ARCH=armv7
    ;;
  *)
    echo "This system's architecture, ${LOCAL_ARCH}, isn't supported"
    exit 1
    ;;
esac

download_failed () {
  printf "Download failed, please make sure your ISTIO_VERSION is correct and verify the download URL exists!"
  exit 1
}

# Downloads the istioctl binary archive.
tmp=$(mktemp -d /tmp/istioctl.XXXXXX)
NAME="istioctl-${ISTIO_VERSION}"

URL="https://github.com/istio/istio/releases/download/${ISTIO_VERSION}/istioctl-${ISTIO_VERSION}-${OSEXT}.tar.gz"
ARCH_URL="https://github.com/istio/istio/releases/download/${ISTIO_VERSION}/istioctl-${ISTIO_VERSION}-${OSEXT}-${ISTIO_ARCH}.tar.gz"

with_arch() {
  printf "\nDownloading %s from %s ...\n" "${NAME}" "$ARCH_URL"
  if ! curl -o /dev/null -sIf "$ARCH_URL"; then
    printf "\n%s is not found, please specify a valid ISTIO_VERSION and TARGET_ARCH\n" "$ARCH_URL"
    exit 1
  fi
  filename="istioctl-${ISTIO_VERSION}-${OSEXT}-${ISTIO_ARCH}.tar.gz"
  curl -fsL -o "${tmp}/${filename}" "$ARCH_URL"
  tar -xzf "${tmp}/${filename}" -C "${tmp}"
}

without_arch() {
  printf "\n Downloading %s from %s ... \n" "${NAME}" "${URL}"
  if ! curl -o /dev/null -sIf "$URL"; then
    printf "\n%s is not found, please specify a valid ISTIO_VERSION\n" "$URL"
    exit 1
  fi
  filename="istioctl-${ISTIO_VERSION}-${OSEXT}.tar.gz"
  curl -fsL -o "${tmp}/${filename}" "$URL"
  tar -xzf "${tmp}/${filename}" -C "${tmp}"
}

# Istio 1.6 and above support arch
# Istio 1.5 and below do not have arch support
ARCH_SUPPORTED="1.6"
# Istio 1.10 and above support arch for osx arm64
ARCH_SUPPORTED_OSX="1.10"

if [ "${OS}" = "Linux" ] ; then
  # This checks if ISTIO_VERSION is less than ARCH_SUPPORTED (version-sort's before it)
  if [ "$(printf '%s\n%s' "${ARCH_SUPPORTED}" "${ISTIO_VERSION}" | sort -V | head -n 1)" = "${ISTIO_VERSION}" ]; then
    without_arch
  else
    with_arch
  fi
elif [ "${OS}" = "Darwin" ] ; then
  # This checks if ISTIO_VERSION is less than ARCH_SUPPORTED_OSX (version-sort's before it) or ISTIO_ARCH not equal to arm64
  if [ "$(printf '%s\n%s' "${ARCH_SUPPORTED_OSX}" "${ISTIO_VERSION}" | sort -V | head -n 1)" = "${ISTIO_VERSION}" ] || [ "${ISTIO_ARCH}" != "arm64" ]; then
    without_arch
  else
    with_arch
  fi
else
  download_failed
fi

printf "%s download complete!\n" "${filename}"

# setup istioctl
mkdir -p "$HOME/.istioctl/bin"
mv "${tmp}/istioctl" "$HOME/.istioctl/bin/istioctl"
chmod +x "$HOME/.istioctl/bin/istioctl"
rm -r "${tmp}"

# Print message
printf "\n"
printf "Add the istioctl to your path with:"
printf "\n"
printf "  export PATH=\$HOME/.istioctl/bin:\$PATH \n"
printf "\n"
printf "Begin the Istio pre-installation check by running:\n"
printf "\t istioctl x precheck \n"
printf "\n"
printf "Need more information? Visit https://istio.io/docs/reference/commands/istioctl/ \n"
