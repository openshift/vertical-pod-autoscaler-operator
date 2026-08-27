# AGENTS.md — openshift/vertical-pod-autoscaler-operator

This file provides AI-specific guidance for working in the Vertical Pod Autoscaler Operator. For contribution guidelines, see [CONTRIBUTING.md](CONTRIBUTING.md).

## Project Overview

This repo is the **Vertical Pod Autoscaler (VPA) Operator** for OpenShift. It deploys and configures the three VPA controller binaries from [openshift/kubernetes-autoscaler](https://github.com/openshift/kubernetes-autoscaler):

| Controller | Role |
|------------|------|
| Recommender | Monitors resource usage and publishes recommendations |
| Admission plugin | Sets resource requests on new or restarted pods |
| Updater | Evicts or in-place updates pods whose requests differ from recommendations |

The operator watches a singleton `VerticalPodAutoscalerController` CR named `default` in the managed namespace (`openshift-vertical-pod-autoscaler` by default). That CR maps to command-line arguments and deployment configuration for the three operand deployments.

End users create `VerticalPodAutoscaler` objects (from the upstream VPA API group `autoscaling.k8s.io`) to opt workloads into autoscaling. The operator does not reconcile those objects directly. It only manages the cluster-level VPA installation.

## Repository Structure

```text
api/v1/                          # VerticalPodAutoscalerController CRD types
cmd/main.go                      # Operator entry point
internal/
  controller/verticalpodautoscaler/  # Main reconciliation loop
  operator/                      # Operator config from environment, status, infrastructure
  lib/resourcemerge/             # Merge helpers for operand objects
  util/                          # TLS and misc helpers
  version/                       # Build version injection
config/
  crd/bases/                     # Generated VerticalPodAutoscalerController CRD
  manager/                       # Operator Deployment kustomize overlay
  manifests/                     # OLM ClusterServiceVersion base
  vpa/                           # Operand RBAC and VPA CRD (synced with kubernetes-autoscaler)
  olm-catalog/                   # CatalogSource kustomize overlay
  samples/                       # Example VerticalPodAutoscalerController
bundle/                          # Generated OLM bundle (do not hand-edit)
hack/
  manifest-diff-upstream.sh      # CI check: operand manifests match kubernetes-autoscaler
  e2e.sh                         # E2E entrypoint for operator and upstream VPA tests
test/
  e2e/                           # Operator-specific e2e tests
  helpers/                       # Shared test utilities
```

## Architecture: What Is Not Obvious

### Reconciliation flow

1. The controller reads the `default` `VerticalPodAutoscalerController` in the watch namespace.
2. It creates or updates three operand Deployments (recommender, admission plugin, updater), a webhook Service, TLS secrets/configmaps.
3. Operator and operand RBAC is reconciled by OLM, not the operator itself.
4. `recommendationOnly: true` on the CR suppresses the updater and admission plugin. Only the recommender runs.
5. Operand container images come from the `VPA_OPERAND_IMAGE` environment variable on the operator Deployment, not from the CR spec.
6. On OpenShift, the admission webhook Service uses serving-cert annotations. The controller wires TLS via `controller-runtime-common` and `library-go` crypto helpers.

### Operand / operator split

Controller binaries and their RBAC live in [openshift/kubernetes-autoscaler](https://github.com/openshift/kubernetes-autoscaler) under `vertical-pod-autoscaler/`. This operator repo carries deployment manifests in `config/vpa/` and reconciles them at runtime.

`hack/manifest-diff-upstream.sh` compares `config/vpa/` against the branch of `openshift/kubernetes-autoscaler` that the operator is synced with. Update `operand_branch` in that script when the operand sync target moves.

### Singleton CR contract

The operator only reconciles the CR named `default`. Other `VerticalPodAutoscalerController` objects are ignored. The watch namespace is set by `WATCH_NAMESPACE` on the operator pod.

## Common Pitfalls

1. **Do not hand-edit generated files.** `api/v1/zz_generated.deepcopy.go`, `config/crd/bases/`, and everything under `bundle/manifests/` are generated. Run `make generate`, `make manifests`, and `make bundle` instead.

2. **Keep operand manifests in sync with kubernetes-autoscaler.** Changes to `config/vpa/vpa-rbac.yaml` or `config/vpa/vpa-crd.yaml` must match the corresponding files in the operand repo. Run `make manifest-diff` before pushing. OpenShift-specific RBAC deltas belong in `hack/filter-upstream-rbac.jq`.

3. **Do not assume all three controllers always run.** Check `recommendationOnly` handling in `verticalpodautoscaler_controller.go` before adding logic that depends on the updater or admission plugin.

4. **Bundle generation has side effects.** `make bundle` can potentially edit `config/manager/kustomization.yaml` if you specify an override environment variable such as `OPERATOR_IMG=...` for local testing purposes.
Revert unintended kustomize edits before committing.

5. **E2E tests clone upstream VPA tests.** `hack/e2e.sh` vendors upstream e2e tests from `kubernetes-autoscaler`. The `operator` suite runs `./test/e2e/` in this repo. Other suites run upstream tests with Ginkgo focus filters.

6. **Float fields in the API.** The `VerticalPodAutoscalerController` spec uses `float64` for recommender tuning fields. `make manifests` requires `crd:allowDangerousTypes=true` because of this.

7. **CI skips doc-only PRs.** Jobs use `skip_if_only_changed` for `*.md` files. Markdown-only changes will not exercise unit, lint, or e2e jobs.

## Human-in-the-Loop Triggers

Stop and consult a human before:

- **Modifying CRD API types** (`api/v1/`) — API changes affect OLM CSV descriptors, bundle generation, and cluster upgrades
- **Changing RBAC** in `config/vpa/` or operator RBAC — privilege changes need security review and operand sync
- **Changing webhook TLS behavior** — affects admission on every pod create/update in clusters with VPA enabled
- **Bumping the operand sync branch** in `hack/manifest-diff-upstream.sh`
- **Changing anything in bundle/** — some files are needed by the ART team, and some are auto-generated
- **Operand image or deployment topology changes** that should be coordinated with `kubernetes-autoscaler`

## Paired Changes

| If you change... | Also update... |
|-----------------|----------------|
| API types in `api/v1/` | Run `make generate && make manifests && make bundle`; update CSV if new spec fields need OLM descriptors |
| Operand RBAC or VPA CRD in `config/vpa/` | Verify with `make manifest-diff`; coordinate with `openshift/kubernetes-autoscaler` |
| OpenShift-specific RBAC filtering | `hack/filter-upstream-rbac.jq` |
| Operator Deployment env vars | `config/manager/manager.yaml`, `Makefile` `predeploy` target, and CI if image substitution changes |
| E2E suite behavior | `hack/e2e.sh` and CI job definitions in openshift/release |

## Further Reading

- [CONTRIBUTING.md](CONTRIBUTING.md) — PR workflow, test expectations, pre-submit checklist
- [README.md](README.md) — Deployment and local development
- Operand source: [openshift/kubernetes-autoscaler/vertical-pod-autoscaler](https://github.com/openshift/kubernetes-autoscaler/tree/main/vertical-pod-autoscaler)
- CI config: [openshift/release/.../vertical-pod-autoscaler-operator/](https://github.com/openshift/release/tree/main/ci-operator/config/openshift/vertical-pod-autoscaler-operator)
