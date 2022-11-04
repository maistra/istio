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

client_gen = client-gen
lister_gen = lister-gen
informer_gen = informer-gen

kube_base_output_package = istio.io/istio/pkg/servicemesh
kube_api_base_package = $(kube_base_output_package)/apis
kube_api_packages = $(kube_api_base_package)/servicemesh/v1
kube_clientset_package = $(kube_base_output_package)/clientset

# file header text
kube_go_header_text = pkg/servicemesh/header.go.txt
# clientset name used by kubernetes client-gen
kube_clientset_name = versioned
# base output package used by kubernetes client-gen
kube_clientset_package = $(kube_base_output_package)/client/clientset
# base output package used by kubernetes lister-gen
kube_listers_package = $(kube_base_output_package)/client/listers
# base output package used by kubernetes informer-gen
kube_informers_package = $(kube_base_output_package)/client/informers

ifeq ($(IN_BUILD_CONTAINER),1)
	# k8s code generators rely on GOPATH, using $GOPATH/src as the base package
	# directory.  Using --output-base . does not work, as that ends up generating
	# code into ./<package>, e.g. ./istio.io/client-go/pkg/apis/...  To work
	# around this, we'll just let k8s generate the code where it wants and copy
	# back to where it should have been generated.
	move_generated=cp -r $(GOPATH)/src/$(kube_base_output_package)/ ./pkg && rm -rf $(GOPATH)/src/$(kube_base_output_package)/
else
	# nothing special for local builds
	move_generated=
endif

.PHONY: maistra-gen
maistra-gen:
	@$(client_gen) --clientset-name $(kube_clientset_name) --input-base "" --input  $(kube_api_packages) --output-package $(kube_clientset_package) -h $(kube_go_header_text)
	@$(lister_gen) --input-dirs $(kube_api_packages) --output-package $(kube_listers_package) -h $(kube_go_header_text)
	@$(informer_gen) --input-dirs $(kube_api_packages) --versioned-clientset-package $(kube_clientset_package)/$(kube_clientset_name) --listers-package $(kube_listers_package) --output-package $(kube_informers_package) -h $(kube_go_header_text)
	@$(move_generated)
