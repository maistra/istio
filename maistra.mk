# Container images labels
MAISTRA_VERSION = "2.2.0"
ISTIO_VERSION = "1.12.7"
OPENSHIFT_TAGS = "istio"
MAINTAINER = "Istio Feedback <istio-feedback@redhat.com>"

# Maistra specific vars
MAISTRA_IMAGES ?= maistra-image.pilot maistra-image.proxy maistra-image.cni

# This target will set BUILD_LABELS variable 
# that allows adding labels to the component container image at building time
.PHONY: component-labels
component-labels: BUILD_LABELS=--label name=$(COMPONENT_NAME) \
												--label com.redhat.component=$(COMPONENT_ID) \
												--label version=$(MAISTRA_VERSION) \
												--label istio_version=$(ISTIO_VERSION) \
												--label summary=$(COMPONENT_SUMMARY) \
												--label description=$(COMPONENT_DESCRIPTION) \
												--label io.k8s.display-name=$(COMPONENT_DISPLAY_NAME) \
												--label io.openshift.tags=$(OPENSHIFT_TAGS) \
												--label maintainer=$(MAINTAINER) \
												--label io.openshift.expose-services=$(COMPONENT_EXPOSE_SVC)
component-labels:
	$(eval BUILD_LABELS = $(BUILD_LABELS))

# This target will set the labels for Maistra Pilot
.PHONY: maistra-pilot-labels
maistra-pilot-labels: COMPONENT_NAME="openshift-service-mesh/pilot-rhel8"
maistra-pilot-labels: COMPONENT_ID="openshift-istio-pilot-rhel8-container"
maistra-pilot-labels: COMPONENT_SUMMARY="Maistra Pilot OpenShift container image"
maistra-pilot-labels: COMPONENT_DISPLAY_NAME="Maistra Pilot"
maistra-pilot-labels: COMPONENT_EXPOSE_SVC="15003:tcp,15005:tcp,15007:tcp,15010:tcp,15011:tcp,8080:tcp,9093:tcp"
maistra-pilot-labels: component-labels

# This target will set the labels for Maistra Proxy
.PHONY: maistra-proxy-labels
maistra-proxy-labels: COMPONENT_NAME="openshift-service-mesh/istio-proxy-rhel8"
maistra-proxy-labels: COMPONENT_ID="openshift-istio-proxy-rhel8-container"
maistra-proxy-labels: COMPONENT_SUMMARY="Maistra Proxy OpenShift container image"
maistra-proxy-labels: COMPONENT_DISPLAY_NAME="Maistra Proxy"
maistra-proxy-labels: COMPONENT_EXPOSE_SVC=""
maistra-proxy-labels: component-labels

# This target will set the labels for Maistra CNI
.PHONY: maistra-cni-labels
maistra-cni-labels: COMPONENT_NAME="openshift-service-mesh/istio-cni-rhel8"
maistra-cni-labels: COMPONENT_ID="openshift-istio-cni-rhel8-container"
maistra-cni-labels: COMPONENT_SUMMARY="Maistra CNI plugin installer OpenShift container image"
maistra-cni-labels: COMPONENT_DISPLAY_NAME="Maistra CNI plugin installer"
maistra-cni-labels: COMPONENT_EXPOSE_SVC=""
maistra-cni-labels: component-labels

# Building container image for specific Maistra component
MAISTRA_IMAGE_RULE ?= time (mkdir -p $(DOCKER_BUILD_TOP)/$@ && TARGET_ARCH=$(TARGET_ARCH) ./tools/docker-copy.sh $^ $(DOCKER_BUILD_TOP)/$@ && cd $(DOCKER_BUILD_TOP)/$@ $(BUILD_PRE) && $(CONTAINER_CLI) build $(BUILD_LABELS) $(BUILD_ARGS) -t $(HUB)/$(patsubst %proxyv2,%proxy,$(subst maistra-image.,,$@)):$(TAG) -f Dockerfile$(suffix $@) . )

.PHONY: maistra-image.pilot
maistra-image.pilot: BUILD_PRE=&& chmod 644 envoy_bootstrap.json gcp_envoy_bootstrap.json
maistra-image.pilot: | maistra-pilot-labels
maistra-image.pilot: ${ISTIO_ENVOY_BOOTSTRAP_CONFIG_DIR}/envoy_bootstrap.json
maistra-image.pilot: ${ISTIO_ENVOY_BOOTSTRAP_CONFIG_DIR}/gcp_envoy_bootstrap.json
maistra-image.pilot: $(ISTIO_OUT_LINUX)/pilot-discovery
maistra-image.pilot: $(ISTIO_OUT_LINUX)/mec
maistra-image.pilot: pilot/docker/maistra/Dockerfile.pilot
	$(MAISTRA_IMAGE_RULE)

.PHONY: maistra-image.proxyv2
maistra-image.proxyv2: BUILD_PRE=&& chmod 644 envoy_bootstrap.json gcp_envoy_bootstrap.json
maistra-image.proxyv2: BUILD_ARGS=--build-arg proxy_version=istio-proxy:${PROXY_REPO_SHA} --build-arg istio_version=${ISTIO_VERSION} --build-arg BASE_VERSION=${BASE_VERSION} --build-arg SIDECAR=${SIDECAR}
maistra-image.proxyv2: | maistra-proxy-labels
maistra-image.proxyv2: ${ISTIO_ENVOY_BOOTSTRAP_CONFIG_DIR}/envoy_bootstrap.json
maistra-image.proxyv2: ${ISTIO_ENVOY_BOOTSTRAP_CONFIG_DIR}/gcp_envoy_bootstrap.json
maistra-image.proxyv2: $(ISTIO_ENVOY_LINUX_RELEASE_DIR)/${SIDECAR}
maistra-image.proxyv2: $(ISTIO_OUT_LINUX)/pilot-agent
maistra-image.proxyv2: pilot/docker/Dockerfile.proxyv2
maistra-image.proxyv2: $(ISTIO_ENVOY_LINUX_RELEASE_DIR)/stats-filter.wasm
maistra-image.proxyv2: $(ISTIO_ENVOY_LINUX_RELEASE_DIR)/stats-filter.compiled.wasm
maistra-image.proxyv2: $(ISTIO_ENVOY_LINUX_RELEASE_DIR)/metadata-exchange-filter.wasm
maistra-image.proxyv2: $(ISTIO_ENVOY_LINUX_RELEASE_DIR)/metadata-exchange-filter.compiled.wasm
	$(MAISTRA_IMAGE_RULE)

# Alias of maistra-image.proxyv2
.PHONY: maistra-image.proxy
maistra-image.proxy: maistra-image.proxyv2

.PHONY: maistra-image.cni
maistra-image.cni: BUILD_ARGS=--build-arg BASE_VERSION=${BASE_VERSION}
maistra-image.cni: | maistra-cni-labels
maistra-image.cni: $(ISTIO_OUT_LINUX)/istio-cni
maistra-image.cni: $(ISTIO_OUT_LINUX)/istio-iptables
maistra-image.cni: $(ISTIO_OUT_LINUX)/install-cni
maistra-image.cni: $(ISTIO_OUT_LINUX)/istio-cni-taint
maistra-image.cni: cni/deployments/kubernetes/maistra/Dockerfile.cni
	$(MAISTRA_IMAGE_RULE)

# for each maistra-image.XXX target create a push.maistra-image.XXX target that pushes
# the local container image to another hub
# a possible optimization is to use tag.$(TGT) as a dependency to do the tag for us
$(foreach TGT,$(MAISTRA_IMAGES),$(eval push.$(TGT): | $(TGT) ; \
	time (set -e; $(CONTAINER_CLI) push $(HUB)/$(subst maistra-image.,,$(TGT)):$(TAG);)))

# This target will build locally all the container images
.PHONY: maistra-image
maistra-image: $(MAISTRA_IMAGES)

PUSH_MAISTRA_IMAGES:=
$(foreach TGT,$(MAISTRA_IMAGES),$(eval PUSH_MAISTRA_IMAGES+=push.$(TGT)))

# This target will build and push all the container images
.PHONY: maistra-image.push
maistra-image.push: $(PUSH_MAISTRA_IMAGES)

.PHONY: vendor
vendor:
	@echo "updating vendor"
	@go mod vendor
	@echo "done updating vendor"

gen: vendor

AGENT_BINARIES += ./mec/cmd/mec
