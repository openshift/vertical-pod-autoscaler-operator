# Must be semver compliant
export OPERATOR_VERSION ?= 4.22.0
OPERATOR_PKG_NAME ?= vertical-pod-autoscaler
IMAGE_VERSION ?= $(OPERATOR_VERSION)
BUNDLE_VERSION ?= $(IMAGE_VERSION)
BUNDLE_MANIFESTS_DIR ?= $(shell pwd)/bundle/manifests
OLM_MANIFESTS_DIR ?= $(shell pwd)/config/olm-catalog

# We used to inject the git hash for version before https://github.com/openshift/vertical-pod-autoscaler-operator/pull/169, accidentally
# removed it. This is for parity with the old behavior, should give us e.g. "v4.21.0-2332559":
INJECT_VERSION     ?= v${OPERATOR_VERSION}-$(shell git rev-parse --short=7 HEAD)
# version used to be in pkg, but now it's internal 
LD_FLAGS    ?= -X github.com/openshift/vertical-pod-autoscaler-operator/internal/version.Raw=$(INJECT_VERSION)

OUTPUT_DIR := ./_output
OLM_OUTPUT_DIR := $(OUTPUT_DIR)/olm-catalog

# Change DEPLOY_NAMESPACE to the namespace where the operator will be deployed.
DEPLOY_NAMESPACE ?= openshift-vertical-pod-autoscaler

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
CHANNELS ?= alpha,beta,stable
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
DEFAULT_CHANNEL ?= stable
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# quay.io/openshift/origin-vertical-pod-autoscaler-operator-bundle:$VERSION and quay.io/openshift/origin-vertical-pod-autoscaler-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= quay.io/openshift/origin-vertical-pod-autoscaler-operator

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:$(BUNDLE_VERSION)

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(BUNDLE_VERSION) $(BUNDLE_METADATA_OPTS) --extra-service-accounts=vpa-admission-controller,vpa-recommender,vpa-updater

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

# Set the Operator SDK version to use. By default, what is installed on the system is used.
# This is useful for CI or a project to utilize a specific version of the operator-sdk toolkit.
OPERATOR_SDK_VERSION ?= v1.41.1

# Image URL to use all building/pushing image targets
OPERATOR_IMG ?= $(IMAGE_TAG_BASE):$(IMAGE_VERSION)
# OPERAND_IMG is the image used for the VerticalPodAutoscaler operand
OPERAND_IMG ?= quay.io/openshift/origin-vertical-pod-autoscaler:latest
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.30

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. docker, podman, buildah)
CONTAINER_TOOL ?= podman

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

# TODO(macao): Running with allowDangerousTypes=true to allow float in controller spec; consider refactoring to string
.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=vertical-pod-autoscaler-operator crd:allowDangerousTypes=true webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: manifest-diff
manifest-diff: build-testutil ## Compare permissions and CRDs from upstream manifests.
	hack/manifest-diff-upstream.sh

# yamllint source is here: https://github.com/adrienverge/yamllint
.PHONY: yamllint
yamllint: ## Run yamllint against manifests.
	hack/yaml-lint.sh

# TODO(macao): Future task to migrate to using envtest https://sdk.operatorframework.io/docs/building-operators/golang/testing/
.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out
	
.PHONY: lint
lint: golangci-lint ## Run golangci-lint.
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint and perform fixes.
	$(GOLANGCI_LINT) run --fix

.PHONY: test-scorecard
test-scorecard: operator-sdk ## Run the scorecard tests. Requires an OpenShift cluster.
	$(OPERATOR_SDK) scorecard bundle -n default -w 300s

.PHONY: check
check: manifest-diff lint yamllint test ## Run quick checks for a dev before pushing code.

.PHONY: ensure-commands-are-noops ## Ensure that make generate and bundle are no-ops.
ensure-commands-are-noops: generate bundle
	@git diff -s --exit-code api/v1/zz_generated.*.go || (echo "Build failed: a model has been changed but the generated resources aren't up to date. Run 'make generate' and update your PR." && exit 1)
	@git diff -s --exit-code -I "createdAt" bundle config || (echo "Build failed: the bundle, config files has been changed but the generated bundle, config files aren't up to date. Run 'make bundle' and update your PR." && git -P diff -I "createdAt" bundle config && exit 1)

##@ E2E Tests

.PHONY: test-e2e ## Run e2e tests for a specific suite. Assumes a running OpenShift cluster (KUBECONFIG set), and the VPA is deployed.
test-e2e: SUITE ?= full-vpa ## Test suite (recommender, updater, admission-controller, actuation, full-vpa)
test-e2e:
	hack/e2e.sh ${KUBECTL} ${SUITE}

.PHONY: e2e-ci
e2e-ci: KUBECTL=$(shell which oc) ## Run e2e tests in CI.
e2e-ci: deploy test-e2e

## Run e2e tests locally. Assumes a running Kubernetes cluster (KUBECONFIG set), and the operator is deployed.
.PHONY: e2e-local
e2e-local: docker-build docker-push
e2e-local: deploy test-e2e

## Requires prior steps that are not reflected in this Makefile, but are present in ci-operator config. You should not run this locally on its own.
.PHONY: e2e-olm-ci
e2e-olm-ci: KUBECTL=$(shell which oc) ## Run e2e tests with OLM in CI.
e2e-olm-ci: test-e2e

## Requires the following environment variables to be set:
## - KUBECONFIG: The path to the kubeconfig file.
## - OPERATOR_IMG: The operator image to be used.
## - BUNDLE_IMG: The bundle image to be used.
## - CATALOG_IMG: The catalog image to be used.
.PHONY: e2e-olm-local
e2e-olm-local: full-olm-deploy test-e2e

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -ldflags "$(LD_FLAGS)" -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

.PHONY: container-binary-build
container-binary-build: ## Build the manager binary for Docker (with out manifest/fmt/vet/etc)
	go build -mod=vendor -a -ldflags "$(LD_FLAGS)" -o manager cmd/main.go

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${OPERATOR_IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${OPERATOR_IMG}

# PLATFORMS defines the target platforms for the manager image be built to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have enabled BuildKit. More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image to your registry (i.e. if you do not set a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To adequately provide solutions that are compatible with multiple platforms, you should consider using this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- $(CONTAINER_TOOL) buildx create --name project-v3-builder
	$(CONTAINER_TOOL) buildx use project-v3-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${OPERATOR_IMG} -f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm project-v3-builder
	rm Dockerfile.cross

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = true
endif

.PHONY: install
install: uninstall manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: predeploy
predeploy: yq ## Setup configuration for the operator before deployment.
	cd config/manager && $(KUSTOMIZE) edit set image quay.io/openshift/origin-vertical-pod-autoscaler-operator=$(OPERATOR_IMG)
	$(YQ) eval 'del(.patches)' -i config/manager/kustomization.yaml
	cd config/manager && $(KUSTOMIZE) edit add patch --patch "[{\"op\":\"replace\",\"path\":\"/spec/template/spec/containers/0/env/0\",\"value\":{\"name\":\"VPA_OPERAND_IMAGE\",\"value\":\"$(OPERAND_IMG)\"}}]" --kind Deployment --version v1 --group apps --name vertical-pod-autoscaler-operator
	cd config/default && $(KUSTOMIZE) edit set namespace $(DEPLOY_NAMESPACE)

.PHONY: deploy
deploy: manifests kustomize predeploy undeploy ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | $(KUBECTL) apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

# Requires BUNDLE_IMG to be set to the bundle image to be used.
.PHONY: deploy-bundle
deploy-bundle: operator-sdk undeploy-bundle delete-ns create-ns ## Deploy the controller in the bundle format with OLM.
	$(OPERATOR_SDK) run bundle $(BUNDLE_IMG) --namespace $(DEPLOY_NAMESPACE) --security-context-config restricted --timeout 5m

.PHONY: undeploy-bundle
undeploy-bundle: operator-sdk ## Undeploy the controller in the bundle format with OLM.
	$(OPERATOR_SDK) cleanup $(OPERATOR_PKG_NAME) -n $(DEPLOY_NAMESPACE)
	$(MAKE) delete-ns

.PHONY: create-ns
create-ns: ## Create the namespace where the operator will be deployed. Ignore if the namespace already exists.
	$(KUBECTL) create ns $(DEPLOY_NAMESPACE) --dry-run=client -o yaml | kubectl apply -f -

.PHONY: delete-ns
delete-ns: ## Delete the namespace where the operator was deployed.
	$(KUBECTL) delete ns $(DEPLOY_NAMESPACE) --ignore-not-found=$(ignore-not-found)

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint

## Tool Versions
KUSTOMIZE_VERSION ?= v5.4.2
CONTROLLER_TOOLS_VERSION ?= v0.17.0 # This is not in sync with operator-sdk, but we need this bumped for compat with k8s 1.32.0
ENVTEST_VERSION ?= release-0.18
GOLANGCI_LINT_VERSION ?= v1.63.4 # This is not in sync with operator-sdk, but we need this bumped for compat with go1.23

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary (ideally with version)
# $2 - package url which can be installed
# $3 - specific version of package
# GOFLAGS='' is explicitly empty here so that we do not run into https://github.com/golang/go/issues/45811 on CI
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
package=$(2)@$(3) ;\
rm -f $(1) || true ;\
echo "Downloading $${package}" ;\
GOBIN=$(LOCALBIN)  GOFLAGS='' go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef

.PHONY: operator-sdk
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk
operator-sdk: ## Download operator-sdk locally if necessary.
ifeq (,$(wildcard $(OPERATOR_SDK)))
ifeq (, $(shell which operator-sdk 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPERATOR_SDK)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPERATOR_SDK) https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk_$${OS}_$${ARCH} ;\
	chmod +x $(OPERATOR_SDK) ;\
	}
else
OPERATOR_SDK = $(shell which operator-sdk)
endif
endif

.PHONY: build-testutil
build-testutil: bin/yaml2json bin/json2yaml ## Build utilities needed by manifest-diff-upstream.sh

bin/yaml2json: cmd/testutil/yaml2json/yaml2json.go
	mkdir -p bin
	go build -o bin/ "$(shell pwd)/cmd/testutil/yaml2json"
bin/json2yaml: cmd/testutil/json2yaml/json2yaml.go
	mkdir -p bin
	go build -o bin/ "$(shell pwd)/cmd/testutil/json2yaml"

# yq from https://github.com/mikefarah/yq. Set version to v4.44.3.
.PHONY: yq
YQ = $(LOCALBIN)/yq
yq: ## Download yq locally if necessary.
ifeq (,$(wildcard $(YQ)))
ifeq (,$(shell which yq 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(YQ)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(YQ) https://github.com/mikefarah/yq/releases/download/v4.44.3/yq_$${OS}_$${ARCH} ;\
	chmod +x $(YQ) ;\
 	}
else
YQ = $(shell which yq)
endif
endif

# VerticalPodAutoscaler is a "supported" resource that operator-sdk will add to the bundle. We need to remove the example manually.
# https://github.com/operator-framework/operator-registry/blob/master/pkg/lib/bundle/supported_resources.go#L18
.PHONY: bundle
bundle: manifests kustomize predeploy operator-sdk ## Generate bundle manifests and metadata, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests
	# Remove old bundle dir in case there were files in the old collection that wouldn't be in the new
	rm -rf $(BUNDLE_MANIFESTS_DIR)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)
	$(OPERATOR_SDK) bundle validate ./bundle

	rm -f $(BUNDLE_MANIFESTS_DIR)/myapp-vpa_autoscaling.k8s.io_v1_verticalpodautoscaler.yaml

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	$(CONTAINER_TOOL) build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push OPERATOR_IMG=$(BUNDLE_IMG)

.PHONY: opm
OPM = $(LOCALBIN)/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.23.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:$(BUNDLE_VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool $(CONTAINER_TOOL) --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push OPERATOR_IMG=$(CATALOG_IMG)

# Recreate all the steps to deploy the operator with OLM using a fully built and pushed catalog image.
# Requires the following environment variables to be set:
# - OPERATOR_IMG: The operator image to be used. Default is 'quay.io/openshift/vertical-pod-autoscaler-operator:$OPERATOR_VERSION'.
# - CATALOG_IMG: The catalog image to be used. Default is 'quay.io/openshift/vertical-pod-autoscaler-operator-catalog:$OPERATOR_VERSION'.
# - BUNDLE_IMG: The bundle image to be used. Default is 'quay.io/openshift/vertical-pod-autoscaler-operator-bundle:$OPERATOR_VERSION'.
# Optional:
# - OPERAND_IMG: The operand image to be used. Default is 'quay.io/openshift/origin-vertical-pod-autoscaler-operator:latest'.
# - DEPLOY_NAMESPACE: The namespace where the operator will be deployed. Default is 'openshift-vertical-pod-autoscaler'.

## Optionally, the easiest way to pass IMG arguments is to instead set the following environment variables:
## - IMAGE_TAG_BASE: The base image tag for the operator.
## - OPERATOR_VERSION: The version of the operator.
## e.g. make e2e-olm-local IMAGE_TAG_BASE=quay.io/$(USER)/vertical-pod-autoscaler-operator OPERATOR_VERSION=4.21.0 KUBECONFIG=/path/to/kubeconfig
## This will create OPERATOR_IMG=quay.io/$(IMAGE_TAG_BASE}:4.21.0, BUNDLE_IMG=quay.io/${IMAGE_TAG_BASE}-bundle:4.21.0, and CATALOG_IMG=quay.io/${IMAGE_TAG_BASE}-catalog:4.21.0
.PHONY: full-olm-deploy
full-olm-deploy: build docker-build docker-push bundle bundle-build bundle-push catalog-build catalog-push deploy-catalog ## Fully deploy the catalog source that contains the operator. Builds and pushes the operator, bundle, and catalog images. Undeploy with 'make undeploy-catalog'.

# Requires CATALOG_IMG to be set to the catalog image to be used.
.PHONY: deploy-catalog
deploy-catalog: delete-ns create-ns ## Deploy the CatalogSource and OperatorGroup, along with the Operator Subscription.
	rm -rf $(OLM_OUTPUT_DIR)
	mkdir -p $(OLM_OUTPUT_DIR)
	cp -r $(OLM_MANIFESTS_DIR)/* $(OLM_OUTPUT_DIR)

	cd $(OLM_OUTPUT_DIR) && $(KUSTOMIZE) edit set image quay.io/openshift/origin-vertical-pod-autoscaler-operator-catalog=$(CATALOG_IMG)
	cd $(OLM_OUTPUT_DIR) && $(KUSTOMIZE) edit set namespace $(DEPLOY_NAMESPACE)
	sed -i 's/OPERATOR_NAMESPACE_PLACEHOLDER/OPERATOR_NAMESPACE=$(DEPLOY_NAMESPACE)/' $(OLM_OUTPUT_DIR)/kustomization.yaml
	$(KUSTOMIZE) build $(OLM_OUTPUT_DIR) | $(KUBECTL) apply -f -

.PHONY: undeploy-catalog
undeploy-catalog: ## Undeploy the catalog image.
	cd $(OLM_OUTPUT_DIR) && $(KUSTOMIZE) edit set image quay.io/openshift/origin-vertical-pod-autoscaler-operator-catalog=$(CATALOG_IMG)
	$(KUSTOMIZE) build $(OLM_OUTPUT_DIR) | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -
	$(MAKE) delete-ns
	$(MAKE) uninstall

.PHONY: create_vpa_controller_cr
create_vpa_controller_cr: ## Create a VPA CR.
	$(KUBECTL) apply -f config/samples/autoscaling_v1_verticalpodautoscalercontroller.yaml

.PHONY: delete_vpa_controller_cr
delete_vpa_controller_cr: ## Delete a VPA CR.
	$(KUBECTL) delete -f config/samples/autoscaling_v1_verticalpodautoscalercontroller.yaml

.PHONY: build-installer
build-installer: manifests generate kustomize ## Generate a consolidated YAML with CRDs and deployment.
	mkdir -p dist
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default > dist/install.yaml
