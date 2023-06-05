# Container images labels
MAISTRA_VERSION ?= 2.3.0
ISTIO_VERSION ?= 1.14.3

BASE_DISTRIBUTION ?= ubi8

# Maistra specific vars
MAISTRA_IMAGES ?= maistra-image.pilot maistra-image.proxyv2 maistra-image.istio-cni

# Pre-build and post-build tasks
MAISTRA_PRE_BUILD_SCRIPT ?= ./maistra/scripts/pre-build.sh
MAISTRA_POST_BUILD_SCRIPT ?= ./maistra/scripts/post-build.sh
MAISTRA_PRE_BUILD ?= time ($(MAISTRA_PRE_BUILD_SCRIPT))
MAISTRA_POST_BUILD ?= time (HUB=$(HUB) TAG=$(TAG) BASE_DISTRIBUTION=$(BASE_DISTRIBUTION) $(MAISTRA_POST_BUILD_SCRIPT) $(ISTIO_COMPONENT))

.PHONY: maistra-image.pilot
maistra-image.pilot: $(export MAISTRA_VERSION)
maistra-image.pilot: ISTIO_COMPONENT=$(subst maistra-image.,,$@)
maistra-image.pilot: 
	$(MAISTRA_PRE_BUILD)
	$(MAKE) docker.pilot
	$(MAISTRA_POST_BUILD)

.PHONY: maistra-image.proxyv2
maistra-image.proxyv2: $(export MAISTRA_VERSION)
maistra-image.proxyv2: ISTIO_COMPONENT=$(subst maistra-image.,,$@)
maistra-image.proxyv2: 
	$(MAISTRA_PRE_BUILD)
	$(MAKE) docker.proxyv2
	$(MAISTRA_POST_BUILD)

.PHONY: maistra-image.istio-cni
maistra-image.istio-cni: $(export MAISTRA_VERSION)
maistra-image.istio-cni: ISTIO_COMPONENT=$(subst maistra-image.,,$@)
maistra-image.istio-cni: 
	$(MAISTRA_PRE_BUILD)
	$(MAKE) docker.install-cni
	$(MAISTRA_POST_BUILD)

# for each maistra-image.XXX target create a push.maistra-image.XXX target that pushes
# the local container image to another hub
# a possible optimization is to use tag.$(TGT) as a dependency to do the tag for us
$(foreach TGT,$(MAISTRA_IMAGES),$(eval push.$(TGT): | $(TGT) ; \
	time (set -e; $(CONTAINER_CLI) push $(HUB)/$(subst maistra-image.,,$(TGT))-$(BASE_DISTRIBUTION):$(TAG);)))

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
