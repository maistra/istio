# Container images labels
MAISTRA_VERSION ?= 2.2.0
ISTIO_VERSION ?= 1.12.7

# Maistra specific vars
MAISTRA_IMAGES ?= maistra-image.pilot maistra-image.proxy maistra-image.cni

# Building container image for specific Maistra component
MAISTRA_IMAGE_RULE ?= time (mkdir -p $(DOCKER_BUILD_TOP)/$@ && TARGET_ARCH=$(TARGET_ARCH) ./tools/docker-copy.sh $^ $(DOCKER_BUILD_TOP)/$@ && cd $(DOCKER_BUILD_TOP)/$@ $(BUILD_PRE) && $(CONTAINER_CLI) build $(BUILD_ARGS) -t $(HUB)/$(subst maistra-image.,,$@):$(TAG) -f Dockerfile$(suffix $@) . )

.PHONY: maistra-image.pilot
maistra-image.pilot: BUILD_PRE=&& chmod 644 envoy_bootstrap.json gcp_envoy_bootstrap.json
maistra-image.pilot: BUILD_ARGS=--build-arg MAISTRA_VERSION=${MAISTRA_VERSION} --build-arg ISTIO_VERSION=${ISTIO_VERSION}
maistra-image.pilot: ${ISTIO_ENVOY_BOOTSTRAP_CONFIG_DIR}/envoy_bootstrap.json
maistra-image.pilot: ${ISTIO_ENVOY_BOOTSTRAP_CONFIG_DIR}/gcp_envoy_bootstrap.json
maistra-image.pilot: $(ISTIO_OUT_LINUX)/pilot-discovery
maistra-image.pilot: $(ISTIO_OUT_LINUX)/mec
maistra-image.pilot: maistra/Dockerfile.pilot
	$(MAISTRA_IMAGE_RULE)

.PHONY: maistra-image.proxy
maistra-image.proxy: BUILD_PRE=&& chmod 644 envoy_bootstrap.json gcp_envoy_bootstrap.json
maistra-image.proxy: BUILD_ARGS=--build-arg proxy_version=istio-proxy:${PROXY_REPO_SHA} --build-arg ISTIO_VERSION=${ISTIO_VERSION} --build-arg SIDECAR=${SIDECAR}
maistra-image.proxy: ${ISTIO_ENVOY_BOOTSTRAP_CONFIG_DIR}/envoy_bootstrap.json
maistra-image.proxy: ${ISTIO_ENVOY_BOOTSTRAP_CONFIG_DIR}/gcp_envoy_bootstrap.json
maistra-image.proxy: $(ISTIO_ENVOY_LINUX_RELEASE_DIR)/${SIDECAR}
maistra-image.proxy: $(ISTIO_OUT_LINUX)/pilot-agent
maistra-image.proxy: maistra/Dockerfile.proxy
maistra-image.proxy: $(ISTIO_ENVOY_LINUX_RELEASE_DIR)/stats-filter.wasm
maistra-image.proxy: $(ISTIO_ENVOY_LINUX_RELEASE_DIR)/stats-filter.compiled.wasm
maistra-image.proxy: $(ISTIO_ENVOY_LINUX_RELEASE_DIR)/metadata-exchange-filter.wasm
maistra-image.proxy: $(ISTIO_ENVOY_LINUX_RELEASE_DIR)/metadata-exchange-filter.compiled.wasm
	$(MAISTRA_IMAGE_RULE)

.PHONY: maistra-image.cni
maistra-image.cni: BUILD_ARGS=--build-arg MAISTRA_VERSION=${MAISTRA_VERSION} --build-arg ISTIO_VERSION=${ISTIO_VERSION}
maistra-image.cni: $(ISTIO_OUT_LINUX)/istio-cni
maistra-image.cni: $(ISTIO_OUT_LINUX)/istio-iptables
maistra-image.cni: $(ISTIO_OUT_LINUX)/install-cni
maistra-image.cni: $(ISTIO_OUT_LINUX)/istio-cni-taint
maistra-image.cni: maistra/Dockerfile.cni
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
