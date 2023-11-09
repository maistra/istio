#!/bin/bash

# Copyright 2019 Istio Authors
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


# Usage: ./integ-suite-kind.sh TARGET
# Example: ./integ-suite-kind.sh test.integration.pilot.kube.presubmit

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

oc version
oc cluster-info

export HUB=quay.io/jwendell TAG=istio-testing
export INTEGRATION_TEST_FLAGS="--istio.test.istio.operatorOptions=components.cni.enabled=true,components.cni.namespace=kube-system --istio.test.kube.helm.values=cni.cniBinDir=/var/lib/cni/bin,cni.cniConfDir=/etc/cni/multus/net.d,cni.chained=false,cni.cniConfFileName=istio-cni.conf,cni.logLevel=info,cni.privileged=true,cni.provider=multus,global.platform=openshift,istio_cni.enabled=true,istio_cni.chained=false"

make test.integration.pilot.kube
