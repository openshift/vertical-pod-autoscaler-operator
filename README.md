# Vertical Pod Autoscaler Operator

The vertical-pod-autoscaler-operator manages deployments and configurations
of the OpenShift [Vertical Pod Autoscaler][1]'s three controllers. The three
controllers are:

* `Recommender`, which monitors the current and past resource consumption and
  provides recommended values for containers' CPU and memory requests.
* `Admission Plugin`, which sets the correct resource requests on new pods using
  data from the Recommender. The recommended request values will be applied to
  new pods which are being restarted (after an eviction by the Updater) or by any
  other pod restart.
* `Updater`, which checks which of the managed pods have incorrect resources set,
  and evicts any it finds so that the pods can be recreated by their
  controllers with the updated resource requests.

[1]: https://github.com/openshift/kubernetes-autoscaler/tree/master/vertical-pod-autoscaler

OpenShift VPA is documented in the [OpenShift product documentation][2].

[2]: https://docs.openshift.com/container-platform/latest/nodes/pods/nodes-pods-vertical-autoscaler.html

## Custom Resource Definitions

The operator manages the following custom resource:

* __VerticalPodAutoscalerController__: This is a singleton resource which
  controls the configuration of the cluster's VPA 3 controller instances.
  The operator will only respond to the VerticalPodAutoscalerController resource named "default" in the
  managed namespace, i.e. the value of the `WATCH_NAMESPACE` environment
  variable.  ([Example][VerticalPodAutoscalerController])

  Many of fields in the spec for VerticalPodAutoscalerController resources correspond to
  command-line arguments of the three VPA controllers and also control which controllers
  should be run.  The example linked above results in the following invocation:

  ```yaml
    Command:
    recommender
    Args:
    --safetyMarginFraction=0.15
    --podMinCPUMillicores=25
    --podMinMemoryMb=250

    Command:
    admission-plugin

    Command:
    updater
  ```

[VerticalPodAutoscalerController]: ./config/samples/autoscaling_v1_verticalpodautoscalercontroller.yaml

## Deployment

### Prerequisites

* `go` version v1.22.0+
* `podman` version 5.1.2+
* [oc](https://docs.openshift.com/container-platform/4.16/cli_reference/openshift_cli/getting-started-cli.html) or `kubectl`
* Access to a Openshift 4.x cluster.

## Setup / Deployment

### Manual Deployment

You can run the operator locally outside of the cluster for development purposes. This is useful for testing changes to the operator code without having to build and deploy a new image to the cluster.

#### Run the operator locally

```sh
make run
```

You can also deploy the operator ot an OpenShift cluster using static manifests. This requires the operator image to be built and pushed to a registry accessible by the cluster.

#### Build and push the operator image

```sh
make docker-build docker-push OPERATOR_IMG=<some-registry>/vertical-pod-autoscaler-operator:tag
```

#### Deploy the operator

```sh
make deploy OPERATOR_IMG=<some-registry>/vertical-pod-autoscaler-operator:tag
```

> [!NOTE]
> This image ought to be published in the personal registry you specified. And it is required to have access to pull the image from the working environment. Make sure you have the proper permission to the registry if the above commands don’t work.

### Bundle Deployment

The operator can be deployed using OLM with `make deploy-bundle`. This will create a bundle deployment to your cluster. You must first build and push your own custom operator image and bundle images to a registry accessible by the cluster.

#### Build and push the bundle image

```sh
make bundle bundle-build bundle-push BUNDLE_IMG=<some-registry>/vertical-pod-autoscaler-operator-bundle:tag
```

> [!NOTE]
> `make bundle` - Generates the bundle manifests and metadata for the operator. This will create a `bundle/` directory in the root of the repository.

Now you can deploy the operator using the bundle image:

```sh
make deploy-bundle \
BUNDLE_IMG=<some-registry>/vertical-pod-autoscaler-operator-bundle:tag \
OPERATOR_IMG=<some-registry>/vertical-pod-autoscaler-operator:tag
```

> [!TIP]
> Note the environment variables for the `deploy-bundle` target:
>
> * `BUNDLE_IMG` - The bundle image to be deployed.
> * `OPERATOR_IMG` - The operator image to be deployed.
> * `OPERAND_IMG` - Optionally, you can specify this environment variable to deploy the operand image.

#### Uninstall bundle

```sh
make undeploy-bundle
```

### Catalog Source Deployment

The operator can also be deployed using OLM with `make deploy-catalog`. This will create a `CatalogSource` with the operator included, as well as deploy a `Subscription` which automatically installs the operator.

For convenience, you can use the `full-olm-deploy` target to build and push the operator image, bundle, build, and push the bundle image, and build, push, and deploy the Catalog image:

```sh
make full-olm-deploy \
IMAGE_TAG_BASE=<some-registry>vertical-pod-autoscaler-operator \
OPERATOR_VERSION=0.0.0-version
```

> [!TIP]
> Instead of defining the `[OPERATOR|BUNDLE|CATALOG]_IMG`s directly, you can use the `IMAGE_TAG_BASE` and `OPERATOR_VERSION` variables to define the image tags for all three images. This will create:
>
> * `OPERATOR_IMG`: `<some-registry>/vertical-pod-autoscaler-operator:0.0.0-version`
> * `BUNDLE_IMG`: `<some-registry>/vertical-pod-autoscaler-operator-bundle:0.0.0-version`
> * `CATALOG_IMG`: `<some-registry>/vertical-pod-autoscaler-operator-catalog:0.0.0-version`

#### Uninstall catalog

```sh
make undeploy-catalog
```

## Lints and Checks

Run `make check` to perform all checks that do not require a cluster.

Here are the individual checks:

* `make fmt` - Run `go fmt` against code.
* `make vet` - Run `go vet` against code.
* `make manifest-diff` - Run `hack/manifest-diff-upstream.sh` to check for differences between the upstream VPA manifests and the operator's manifests.
* `make lint` - Run `golangci-lint` against the code.
* `make test` - Run the unit tests for the operator.

## Testing

`make test` runs the unit tests for the operator.

`make test-scorecard` will run Operator SDK's scorecard tests. This requires a Kubernetes or OpenShift cluster to be available and configured in your environment. The tests will be run against the cluster.

`make test-e2e` will run the e2e tests for the operator. These tests assume the presence of a cluster not already running the operator, and that the KUBECONFIG environment variable points to a configuration granting admin rights on said cluster. It assumes the operator is already deployed. If not, the following commands can run the e2e steps in one command:

`make e2e-local` - Manually deploys the operator to the cluster, and runs the e2e tests. Requires:

* `KUBECONFIG` environment variable to be set to a configuration granting admin rights on the cluster.
* `OPERATOR_IMG` environment variable to be set to the operator image to deploy.

`make e2e-olm-local` - Manually deploys the operator to the cluster using OLM, and runs the e2e tests.

* `KUBECONFIG` The path to the kubeconfig file.
* `IMAGE_TAG_BASE` The base image tag for the operator, bundle, and catalog images.
* `OPERATOR_VERSION` The version of the operator to deploy, used to tag the images.

```sh
make e2e-olm-local \
IMAGE_TAG_BASE=<some-registry>vertical-pod-autoscaler-operator \
OPERATOR_VERSION=0.0.0-version \
KUBECONFIG=<path-to-kubeconfig>
```

All tests should clean up after themselves.

> [!TIP]
> The e2e tests clones the upstream VPA repository to a temporary directory and runs upstream tests against the cluster. Optionally, you can specify an environment variable `AUTOSCALER_TMP` to specify an existing directory to use. If specified, the e2e script will checkout the correct branch, and pull. This saves network bandwidth and time from cloning the repository each time.
