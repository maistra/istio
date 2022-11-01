#!/usr/bin/bash
#
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



cd bookinfo/platform/kube/ || exit
# shellcheck disable=SC2013,2035  
for i in $(grep 'image:' *.yaml | cut -d':' -f 1 | sort --unique)
do 
sed -i '/image:/s/2.1/2.1.0-ibm-p/g' "${i}"
done
