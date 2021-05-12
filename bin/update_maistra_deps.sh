#!/bin/bash

# Copyright Red Hat, Inc.
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

set -eux

UPDATE_BRANCH=${UPDATE_BRANCH:-"maistra-2.3"}

# Update go dependencies
go get -d "maistra.io/api@${UPDATE_BRANCH}"
go mod tidy
go mod vendor

# Copy CRD's
dir=$(mktemp -d)
git clone "https://github.com/maistra/api.git" "${dir}"
pushd "${dir}"
git checkout "${UPDATE_BRANCH}"
popd

# FIXME: https://issues.redhat.com/browse/MAISTRA-2353
# For now we are just copying the files that already exist in istio, i.e., we are not adding any new files.
# We should copy all CRD's from api repo, i.e., uncomment the lines below and delete the other copy commands
# rm -f manifests/charts/base/crds/maistra*
# cp "${dir}"/manifests/* manifests/charts/base/crds

cp "${dir}"/manifests/federation.maistra.io_servicemeshpeers.yaml manifests/charts/base/crds
cp "${dir}"/manifests/federation.maistra.io_exportedservicesets.yaml manifests/charts/base/crds
cp "${dir}"/manifests/federation.maistra.io_importedservicesets.yaml manifests/charts/base/crds

rm -rf "${dir}"

# Regenerate files
make clean gen
