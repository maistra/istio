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

oc delete -n mesh1-system meshfederation/mesh2
oc delete -n mesh2-system meshfederation/mesh1

oc delete project mesh1-system
oc delete project mesh2-system
# oc delete project mesh1-exports
# oc delete project mesh2-imports
oc delete project mesh1-bookinfo
oc delete project mesh2-bookinfo
