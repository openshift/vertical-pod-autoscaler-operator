DBG         ?= 0
PROJECT     ?= vertical-pod-autoscaler-operator
ORG_PATH    ?= github.com/openshift
REPO_PATH   ?= $(ORG_PATH)/$(PROJECT)
VERSION     ?= $(shell git describe --always --dirty --abbrev=7)
LD_FLAGS    ?= -X $(REPO_PATH)/pkg/version.Raw=$(VERSION)
BUILD_DEST  ?= bin/vertical-pod-autoscaler-operator
MUTABLE_TAG ?= latest
IMAGE        = origin-vertical-pod-autoscaler-operator

# Add OUTPUT_DIR for various targets
OUTPUT_DIR 						:= ./_output
INSTALL_DIR						:= ./install/
OLM_ARTIFACTS 					:= $(INSTALL_DIR)/olm
KUBE_MANIFESTS_DIR 				:= $(OUTPUT_DIR)/deployment
OPERATOR_REGISTRY_MANIFESTS_DIR := $(OUTPUT_DIR)/olm/registry
OLM_MANIFESTS_DIR 				:= $(OUTPUT_DIR)/olm/subscription

KUBECTL = kubectl
REGISTRY_VERSION	:= 4.8

OPERATOR_NAMESPACE			:= openshift-vertical-pod-autoscaler
OPERATOR_DEPLOYMENT_NAME	:= vertical-pod-autoscaler-operator

export OLD_OPERATOR_IMAGE_URL_IN_CSV 	= quay.io/openshift/vertical-pod-autoscaler-operator:$(REGISTRY_VERSION)
export OLD_OPERAND_IMAGE_URL_IN_CSV 	= quay.io/openshift/vertical-pod-autoscaler:$(REGISTRY_VERSION)
export CSV_FILE_PATH_IN_REGISTRY_IMAGE 	= /manifests/$(REGISTRY_VERSION)/vertical-pod-autoscaler.v$(REGISTRY_VERSION).0.clusterserviceversion.yaml

# build image for ci
CI_REPO ?=registry.svc.ci.openshift.org
$(call build-image,vertical-pod-autoscaler-operator,$(CI_IMAGE_REGISTRY)/autoscaling/vertical-pod-autoscaler-operator,./images/ci/Dockerfile,.)

# Added LOCAL_OPERATOR_IMAGE for local-image build
DEV_REPO			?= quay.io/redhat
DEV_OPERATOR_IMAGE	?= openshift-vertical-pod-autoscaler-operator
DEV_OPERAND_IMAGE	?= openshift-vertical-pod-autoscaler
DEV_REGISTRY_IMAGE	?= vpa-operator-registry

LOCAL_OPERATOR_IMAGE	?= $(DEV_REPO)/$(DEV_OPERATOR_IMAGE):latest
LOCAL_OPERAND_IMAGE 	?= $(DEV_REPO)/$(DEV_OPERAND_IMAGE):latest
LOCAL_OPERATOR_REGISTRY_IMAGE ?= $(DEV_REPO)/$(DEV_REGISTRY_IMAGE):latest
export LOCAL_OPERATOR_IMAGE
export LOCAL_OPERAND_IMAGE
export LOCAL_OPERATOR_REGISTRY_IMAGE

ifeq ($(DBG),1)
GOGCFLAGS ?= -gcflags=all="-N -l"
endif

GO_BUILD_BINDIR := bin
GO_TEST_PACKAGES :=./pkg/... ./cmd/...

.PHONY: all
all: build images check

HASDOCKER := $(shell command -v docker 2> /dev/null)
ifeq ($(HASDOCKER), )
  DOCKER_RUNTIME := podman
  IMAGE_BUILD_CMD := buildah bud
else
  DOCKER_RUNTIME := docker
  IMAGE_BUILD_CMD := docker build
endif

NO_DOCKER ?= 1
ifeq ($(NO_DOCKER), 1)
  DOCKER_CMD =
  IMAGE_BUILD_CMD = imagebuilder
else
  DOCKER_CMD := $(DOCKER_RUNTIME) run --rm -v "$(CURDIR):/go/src/$(REPO_PATH):Z" -w "/go/src/$(REPO_PATH)" openshift/origin-release:golang-1.15
endif
export NO_DOCKER

# Build registry_setup_binary
REGISTRY_SETUP_BINARY := bin/registry-setup
$(REGISTRY_SETUP_BINARY): GO_BUILD_PACKAGES =./test/registry-setup/...
$(REGISTRY_SETUP_BINARY):
	$(DOCKER_CMD) go build $(GOGCFLAGS) -ldflags "$(LD_FLAGS)" -o "$(REGISTRY_SETUP_BINARY)" "$(REPO_PATH)/test/registry-setup"

.PHONY: depend
depend:
	dep version || go get -u github.com/golang/dep/cmd/dep
	dep ensure

.PHONY: depend-update
depend-update:
	dep ensure -update

# This is a hack. The operator-sdk doesn't currently let you configure
# output paths for the generated CRDs.  It also requires that they
# already exist in order to regenerate the OpenAPI bits, so we do some
# copying around here.
.PHONY: generate
generate: ## Code generation (requires operator-sdk >= v0.5.0)
	mkdir -p deploy/crds

	cp install/01_vpacontroller.crd.yaml \
	  deploy/crds/autoscaling_v1_01_vpacontroller.crd.yaml

	operator-sdk generate k8s
	operator-sdk generate openapi

	cp deploy/crds/autoscaling_v1_01_vpacontroller.crd.yaml \
	  install/01_vpacontroller.crd.yaml

.PHONY: build
build: ## build binaries
	@# version must be of the form v1.2.3 with optional suffixes -4 and/or -g56789ab
	@# or the binary will crash when it tries to parse its version.Raw
	@echo $(VERSION) | grep -qP '^v\d+\.\d+\.\d+(-\d+)?(-g[a-f0-9]{7,})?(\.p\d+)?(-dirty)?$$' || \
      			{ echo "Invalid version $(VERSION), cannot build"; false; }
	$(DOCKER_CMD) go build $(GOGCFLAGS) -ldflags "$(LD_FLAGS)" -o "$(BUILD_DEST)" "$(REPO_PATH)/cmd/manager"

# Build image for dev use.
dev-image:
	$(IMAGE_BUILD_CMD) -t "$(DEV_REPO)/$(DEV_OPERATOR_IMAGE):$(MUTABLE_TAG)" ./

dev-push:
	$(DOCKER_RUNTIME) push "$(DEV_REPO)/$(DEV_OPERATOR_IMAGE):$(MUTABLE_TAG)"

.PHONY: images
images: ## Create images
	$(IMAGE_BUILD_CMD) -t "$(IMAGE):$(VERSION)" -t "$(IMAGE):$(MUTABLE_TAG)" ./

.PHONY: push
push:
	$(DOCKER_RUNTIME) push "$(IMAGE):$(VERSION)"
	$(DOCKER_RUNTIME) push "$(IMAGE):$(MUTABLE_TAG)"

.PHONY: check
check: fmt vet lint test manifest-diff ## Check your code

.PHONY: check-pkg
check-pkg:
	./hack/verify-actuator-pkg.sh

.PHONY: test
test: ## Run unit tests
	$(DOCKER_CMD) go test -race -cover ./...

.PHONY: test-e2e
test-e2e: ## Run e2e tests
	hack/e2e.sh $(KUBECTL)

.PHONY: lint
lint: ## Go lint your code
	hack/go-lint.sh -min_confidence 0.3 $(go list -f '{{ .ImportPath }}' ./...)

.PHONY: fmt
fmt: ## Go fmt your code
	hack/go-fmt.sh .

.PHONY: vet
vet: ## Apply go vet to all go files
	hack/go-vet.sh ./...

.PHONY: manifest-diff
manifest-diff: build-testutil ## Compare permissions and CRDs from OLM manifests and install/*.yaml
	hack/manifest-diff.sh

.PHONY: build-testutil
build-testutil: bin/yaml2json bin/json2yaml ## Build utilities needed by tests

# utilities needed by tests
bin/yaml2json: cmd/testutil/yaml2json/yaml2json.go
	mkdir -p bin
	go build $(GOGCFLAGS) -ldflags "$(LD_FLAGS)" -o bin/ "$(REPO_PATH)/cmd/testutil/yaml2json"
bin/json2yaml: cmd/testutil/json2yaml/json2yaml.go
	mkdir -p bin
	go build $(GOGCFLAGS) -ldflags "$(LD_FLAGS)" -o bin/ "$(REPO_PATH)/cmd/testutil/json2yaml"

.PHONY: help
help:
	@grep -E '^[a-zA-Z/0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: clean
clean:  ## Remove build artifacts
	rm -rf $(OUTPUT_DIR) bin

.PHONY: clean-deploy
clean-deploy: ## Uninstall VPA from current cluster
	${KUBECTL} delete mutatingwebhookconfigurations vpa-webhook-config || true
	${KUBECTL} delete ns openshift-vertical-pod-autoscaler || true
	${KUBECTL} delete crd verticalpodautoscalercheckpoints.autoscaling.k8s.io verticalpodautoscalercontrollers.autoscaling.openshift.io verticalpodautoscalers.autoscaling.k8s.io || true

e2e-olm-local: DEPLOY_MODE := local
e2e-olm-local: dev-image dev-push deploy-olm-local test-e2e

e2e-local: DEPLOY_MODE=local
e2e-local: dev-image dev-push deploy test-e2e

e2e-olm-ci: DEPLOY_MODE := ci
e2e-olm-ci: KUBECTL=$(shell which oc)
e2e-olm-ci: deploy-olm-ci test-e2e

e2e-ci: DEPLOY_MODE := ci
e2e-ci: KUBECTL=$(shell which oc)
e2e-ci: deploy test-e2e

deploy-olm-local: operator-registry-deploy-local olm-generate olm-apply
deploy-olm-ci: operator-registry-deploy-ci olm-generate olm-apply

operator-registry-deploy-local: operator-registry-generate operator-registry-image-ci operator-registry-deploy
operator-registry-deploy-ci: operator-registry-generate operator-registry-deploy

# deploy the operator using kube manifests (no OLM)
.PHONY: deploy
deploy: KUBE_MANIFESTS_SOURCE := "$(INSTALL_DIR)/deploy"
deploy: DEPLOYMENT_YAML := "$(KUBE_MANIFESTS_DIR)/03_deployment.yaml"
deploy: CONFIGMAP_ENV_FILE := "$(KUBE_MANIFESTS_DIR)/registry-env.yaml"
deploy: $(REGISTRY_SETUP_BINARY)
deploy:
	rm -rf $(KUBE_MANIFESTS_DIR)
	mkdir -p $(KUBE_MANIFESTS_DIR)
	cp -r $(KUBE_MANIFESTS_SOURCE)/* $(KUBE_MANIFESTS_DIR)/
	cp $(INSTALL_DIR)/registry-env.yaml $(KUBE_MANIFESTS_DIR)/

	$(REGISTRY_SETUP_BINARY) --mode=$(DEPLOY_MODE) --olm=false --configmap=$(CONFIGMAP_ENV_FILE)
	./hack/update-image-url.sh "$(CONFIGMAP_ENV_FILE)" "$(DEPLOYMENT_YAML)"

	# Remove the config env file for non-olm installation.
	rm $(CONFIGMAP_ENV_FILE)

	$(KUBECTL) apply -f $(KUBE_MANIFESTS_DIR)

# apply OLM resources to deploy the operator.
olm-apply:
	$(KUBECTL) apply -n $(OPERATOR_NAMESPACE) -f $(OLM_MANIFESTS_DIR)
	./hack/wait-for-deployment.sh $(KUBECTL) $(OPERATOR_NAMESPACE) $(OPERATOR_DEPLOYMENT_NAME) 500

# generate OLM resources (Subscription and OperatorGroup etc.) to install the operator via olm.
olm-generate: OPERATOR_GROUP_FILE := "$(OLM_MANIFESTS_DIR)/operator-group.yaml"
olm-generate: SUBSCRIPTION_FILE := "$(OLM_MANIFESTS_DIR)/subscription.yaml"
olm-generate:
	rm -rf $(OLM_MANIFESTS_DIR)
	mkdir -p $(OLM_MANIFESTS_DIR)
	cp -r $(OLM_ARTIFACTS)/subscription/* $(OLM_MANIFESTS_DIR)/

	sed "s/OPERATOR_NAMESPACE_PLACEHOLDER/$(OPERATOR_NAMESPACE)/g" -i "$(OPERATOR_GROUP_FILE)"
	sed "s/OPERATOR_NAMESPACE_PLACEHOLDER/$(OPERATOR_NAMESPACE)/g" -i "$(SUBSCRIPTION_FILE)"
	sed "s/OPERATOR_PACKAGE_CHANNEL/\"$(REGISTRY_VERSION)\"/g" -i "$(SUBSCRIPTION_FILE)"

# generate kube resources to deploy operator registry image using an init container.
operator-registry-generate: OPERATOR_REGISTRY_DEPLOYMENT_YAML := "$(OPERATOR_REGISTRY_MANIFESTS_DIR)/registry-deployment.yaml"
operator-registry-generate: CONFIGMAP_ENV_FILE := "$(OPERATOR_REGISTRY_MANIFESTS_DIR)/registry-env.yaml"
operator-registry-generate: $(REGISTRY_SETUP_BINARY)
operator-registry-generate:
	rm -rf $(OPERATOR_REGISTRY_MANIFESTS_DIR)
	mkdir -p $(OPERATOR_REGISTRY_MANIFESTS_DIR)
	cp -r $(OLM_ARTIFACTS)/registry/* $(OPERATOR_REGISTRY_MANIFESTS_DIR)/
	cp $(INSTALL_DIR)/registry-env.yaml $(OPERATOR_REGISTRY_MANIFESTS_DIR)/

	# write image URL(s) into a json file and
	#   IMAGE_FORMAT='registry.svc.ci.openshift.org/ci-op-9o8bacu/stable:${component}'
	sed "s/OPERATOR_NAMESPACE_PLACEHOLDER/$(OPERATOR_NAMESPACE)/g" -i "$(OPERATOR_REGISTRY_DEPLOYMENT_YAML)"
	$(REGISTRY_SETUP_BINARY) --mode=$(DEPLOY_MODE) --olm=true --configmap=$(CONFIGMAP_ENV_FILE)
	./hack/update-image-url.sh "$(CONFIGMAP_ENV_FILE)" "$(OPERATOR_REGISTRY_DEPLOYMENT_YAML)"

# deploy the operator registry image
operator-registry-deploy: CATALOG_SOURCE_TYPE := address
operator-registry-deploy: bin/yaml2json
	$(KUBECTL) delete ns $(OPERATOR_NAMESPACE) || true
	$(KUBECTL) create ns $(OPERATOR_NAMESPACE)
	./hack/deploy-operator-registry.sh $(OPERATOR_NAMESPACE) $(KUBECTL) $(OPERATOR_REGISTRY_MANIFESTS_DIR) ./bin/yaml2json

# build operator registry image for ci locally (used for local e2e test only)
# local e2e test is done exactly the same way as ci with the exception that
# in ci the operator registry image is built by ci agent.
# on the other hand, in local mode, we need to build the image.
operator-registry-image-ci:
	docker build --build-arg VERSION=$(REGISTRY_VERSION) -t $(LOCAL_OPERATOR_REGISTRY_IMAGE) -f images/operator-registry/Dockerfile.registry.ci .
	docker push $(LOCAL_OPERATOR_REGISTRY_IMAGE)

# build and push the OLM manifests for this operator into an operator-registry image.
# this builds an image with the generated database, (unlike image used for ci)
operator-registry-image: MANIFESTS_DIR := $(OUTPUT_DIR)/manifests
operator-registry-image: CSV_FILE := $(MANIFESTS_DIR)/$(REGISTRY_VERSION)/vertical-pod-autoscaler.v$(REGISTRY_VERSION).0.clusterserviceversion.yaml
operator-registry-image:
	rm -rf $(MANIFESTS_DIR)
	mkdir -p $(MANIFESTS_DIR)
	cp manifests/*.package.yaml $(MANIFESTS_DIR)/
	cp -r manifests/[0-9].[0-9]* $(MANIFESTS_DIR)/
	find $(MANIFESTS_DIR)/[0-9].[0-9]* -type f ! -name '*.yaml' | xargs rm -v

	test -n "$(LOCAL_OPERATOR_IMAGE)" || { echo "Unable to find operator image"; false; }
	test -n "$(LOCAL_OPERAND_IMAGE)" || { echo "Unable to find operand image"; false; }

	sed "s,$(OLD_OPERATOR_IMAGE_URL_IN_CSV),$(LOCAL_OPERATOR_IMAGE),g" -i "$(CSV_FILE)"
	sed "s,$(OLD_OPERAND_IMAGE_URL_IN_CSV),$(LOCAL_OPERAND_IMAGE),g" -i "$(CSV_FILE)"

	$(IMAGE_BUILD_CMD) --build-arg MANIFEST_LOCATION=$(MANIFESTS_DIR) -t $(LOCAL_OPERATOR_REGISTRY_IMAGE) -f images/operator-registry/Dockerfile.registry .
	$(DOCKER_RUNTIME) push $(LOCAL_OPERATOR_REGISTRY_IMAGE)
