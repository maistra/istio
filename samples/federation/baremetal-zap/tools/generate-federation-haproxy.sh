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


set +e

# shellcheck disable=SC1091
source common.sh


  MESH1_DISCOVERY_PORT=$(oc1 -n mesh1-system get svc mesh2-ingress -o jsonpath="{.spec.ports[1].nodePort}")
  MESH1_SERVICE_PORT=$(oc1 -n mesh1-system get svc mesh2-ingress -o jsonpath="{.spec.ports[0].nodePort}")

  MESH2_DISCOVERY_PORT=$(oc2 -n mesh2-system get svc mesh1-ingress -o jsonpath="{.spec.ports[1].nodePort}")
  MESH2_SERVICE_PORT=$(oc2 -n mesh2-system get svc mesh1-ingress -o jsonpath="{.spec.ports[0].nodePort}")

sed -e "s:{{MESH1_DISCOVERY_PORT}}:$MESH1_DISCOVERY_PORT:g" \
    -e "s:{{MESH1_SERVICE_PORT}}:$MESH1_SERVICE_PORT:g"     \
    -e "s:{{MESH2_DISCOVERY_PORT}}:$MESH2_DISCOVERY_PORT:g" \
    -e "s:{{MESH2_SERVICE_PORT}}:$MESH2_SERVICE_PORT:g"     \
    federation.cfg.template > federation.cfg

echo "
backend mesh1-service
    mode tcp
    balance source " >> federation.cfg
for NodeName in $(oc1 get nodes -o wide -o jsonpath="{.items[*].status.addresses[1].address}")
do
  NodeIP=$(oc1 get node "${NodeName}" -o wide -o jsonpath="{.status.addresses[0].address}")
  echo "    server      $NodeName ${NodeIP}:${MESH1_SERVICE_PORT} check" >> federation.cfg
done


echo "
backend mesh1-discovery
    mode tcp
    balance source " >> federation.cfg
for NodeName in $(oc1 get nodes -o wide -o jsonpath="{.items[*].status.addresses[1].address}")
do
  NodeIP=$(oc1 get node "${NodeName}" -o wide -o jsonpath="{.status.addresses[0].address}")
  echo "    server      $NodeName ${NodeIP}:${MESH1_DISCOVERY_PORT} check" >> federation.cfg
done

echo "
backend mesh2-service
    mode tcp
    balance source " >> federation.cfg
for NodeName in $(oc2 get nodes -o wide -o jsonpath="{.items[*].status.addresses[1].address}")
do
  NodeIP=$(oc2 get node "${NodeName}" -o wide -o jsonpath="{.status.addresses[0].address}")
  echo "    server      $NodeName ${NodeIP}:${MESH2_SERVICE_PORT} check" >> federation.cfg
done


echo "
backend mesh2-discovery
    mode tcp
    balance source " >> federation.cfg
for NodeName in $(oc2 get nodes -o wide -o jsonpath="{.items[*].status.addresses[1].address}")
do
  NodeIP=$(oc2 get node "${NodeName}" -o wide -o jsonpath="{.status.addresses[0].address}")
  echo "    server      $NodeName ${NodeIP}:${MESH2_DISCOVERY_PORT} check" >> federation.cfg
done
