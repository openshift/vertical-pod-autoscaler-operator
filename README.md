# Vertical Pod Autoscaler Operator

The vertical-pod-autoscaler-operator manages deployments and configurations
of the OpenShift [Vertical Pod Autoscaler][1]'s three controllers. The three
controllers are:
* Recommender, which monitors the current and past resource consumption and
  provides recommended values for containers' CPU and memory requests.
* Admission Plugin, which sets the correct resource requests on new pods using
  data from the Recommender. The recommended request values will be applied to
  new pods which are being restarted (after an eviction by the Updater) or by any
  other pod restart.
* Updater, which checks which of the managed pods have incorrect resources set,
  and evicts any it finds so that the pods can be recreated by their
  controllers with the updated resource requests.

[1]: https://github.com/openshift/kubernetes-autoscaler/tree/master/vertical-pod-autoscaler

OpenShift VPA is documented in the [OpenShift product documentation][2].

[2]: https://docs.openshift.com/container-platform/latest/nodes/pods/nodes-pods-vertical-autoscaler.html

## Custom Resource Definitions

The operator manages the following custom resource:

- __VerticalPodAutoscalerController__: This is a singleton resource which
  controls the configuration of the cluster's VPA 3 controller instances.
  The operator will only respond to the VerticalPodAutoscalerController resource named "default" in the
  managed namespace, i.e. the value of the `WATCH_NAMESPACE` environment
  variable.  ([Example][VerticalPodAutoscalerController])

  Many of fields in the spec for VerticalPodAutoscalerController resources correspond to
  command-line arguments of the three VPA controllers and also control which controllers
  should be run.  The example linked above results in the following invocation:

  ```
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

[VerticalPodAutoscalerController]: https://github.com/openshift/vertical-pod-autoscaler-operator/blob/master/examples/vpacontroller.yaml


## Development

```sh-session
## Build, Test, & Run
$ make build
$ make test

$ export WATCH_NAMESPACE=openshift-vertical-pod-autoscaler
$ ./bin/vertical-pod-autoscaler-operator -alsologtostderr
```

The Vertical Pod Autoscaler Operator is designed to be deployed on
OpenShift by the [Operator Lifecycle Manager][OLM], but it's possible to
run it directly on any vanilla Kubernetes cluster.
To do so, apply the manifests in the `install/deploy` directory:
`kubectl apply -f ./install/deploy`

This will create the `openshift-vertical-pod-autoscaler` namespace, register the
custom resource definitions, configure RBAC policies, and create a
deployment for the operator.

[OLM]: https://docs.openshift.com/container-platform/latest/operators/understanding/olm/olm-understanding-olm.html

### End-to-End Tests

You can run the e2e test suite with `make test-e2e`.  These tests
assume the presence of a cluster not already running the operator, and
that the `KUBECONFIG` environment variable points to a configuration
granting admin rights on said cluster.
