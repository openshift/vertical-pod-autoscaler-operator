# Contributing to Vertical Pod Autoscaler Operator

This document covers contribution guidelines for [openshift/vertical-pod-autoscaler-operator](https://github.com/openshift/vertical-pod-autoscaler-operator).

## Related Resources

| Resource | Link |
|----------|------|
| Operand repo | [openshift/kubernetes-autoscaler](https://github.com/openshift/kubernetes-autoscaler) (VPA controllers live under `vertical-pod-autoscaler/`) |
| CI configuration | [openshift/release/.../vertical-pod-autoscaler-operator/](https://github.com/openshift/release/tree/main/ci-operator/config/openshift/vertical-pod-autoscaler-operator) |
| AI guidance | [AGENTS.md](AGENTS.md) |
| OpenShift docs | [Vertical Pod Autoscaler](https://docs.openshift.com/container-platform/latest/nodes/pods/nodes-pods-vertical-autoscaler.html) |

## Review and Approval Policy

Every change in every pull request must be understood and approved by two humans. This can be the PR author and a reviewer, or, if the author used an AI tool and does not fully understand the contents of the PR, two human reviewers.

**Exception:** PRs authored by deterministic automation tools that are part of our CI and related systems (whose code has been reviewed by the OpenShift engineering org) can be merged with a single human review.

Every change should be closely scrutinized for bugs. Our software is complex with many interdependencies. Review changes from multiple angles:

- **Product architecture**: Does this fit the intended design of the VPA operator and OpenShift VPA?
- **Security**: Are there new attack surfaces, credential handling issues, or privilege escalations?
- **Thread safety**: The operator reconciles multiple deployments and webhook resources concurrently. Are shared resources handled correctly?
- **Regressions**: Could this break existing VPA controller deployments, webhook TLS, or operand upgrades?
- **Effects on other components**: How does this impact the operand in `kubernetes-autoscaler`, cluster upgrades, or OLM bundle generation?

## PR Title Convention

PR titles should be prefixed with a Jira ticket reference:

```text
AUTOSCALE-123: Fix the whatsit in the thingamajig
OCPBUGS-456: Correct nil pointer in webhook reconciliation
NO-JIRA: Update Go module dependencies
```

## PR Workflow

This repo uses [OpenShift CI (Prow)](https://docs.ci.openshift.org/) for continuous integration. PRs are automatically merged once all required tests pass and the correct labels are present.

### Required labels for merge

- `lgtm` — Added by a reviewer via the `/lgtm` command. Any developer from the OpenShift org can add this after reviewing the PR.
- `approved` — Added by an approver listed in the [OWNERS](OWNERS) file via the `/approve` command.
- `verified` — Added by anyone in the OpenShift org, but typically by the PR author.

### Useful commands

Comment these on the PR:

| Command | Effect |
|---------|--------|
| `/lgtm` | Add the `lgtm` label after reviewing |
| `/lgtm cancel` | Remove the `lgtm` label |
| `/approve` | Add the `approved` label (OWNERS approvers only) |
| `/retest` | Re-run all failed required tests |
| `/retest-required` | Re-run only the failed required tests |
| `/test <test-name>` | Run a specific test, e.g. `/test unit` or `/test e2e-aws-olm` |
| `/hold` | Prevent the PR from being merged |
| `/hold cancel` | Remove the hold and allow merging |
| `/verified` | Mark the PR as verified |
| `/cherry-pick release-4.23` | Create a cherry-pick PR to a release branch |
| `/pipeline required` | Manually trigger all required second-stage tests (e.g., E2Es) without waiting for `/lgtm` |

### LGTM mode and E2E tests

Repos enrolled in LGTM mode defer second-stage tests (such as E2Es) until the `lgtm` label is applied. This avoids wasting CI resources on PRs that haven't been reviewed yet. If you need to run E2Es before getting `/lgtm` (e.g., to validate before requesting review), use `/pipeline required`.

### Preventing premature merges

- Add the `WIP:` prefix to the PR title (e.g., `WIP: AUTOSCALE-123: Work in progress`). Prow adds the `do-not-merge/work-in-progress` label automatically.
- Use `/hold` to temporarily block merging while awaiting additional review or testing.

## Test Expectations

PRs should include tests to verify correctness and prevent future regressions:

- **Unit tests**: Required for new logic, bug fixes, and behavior changes. Run with `make test`.
- **Controller envtests**: The controller package uses envtest. Add or update tests in `internal/controller/verticalpodautoscaler/` when reconciliation behavior changes.
- **Manifest diff**: `make manifest-diff` compares operand RBAC and CRDs against [openshift/kubernetes-autoscaler](https://github.com/openshift/kubernetes-autoscaler). Run this when changing `config/vpa/`.
- **E2E tests**: Expected for new features or significant behavior changes. CI runs operator and upstream VPA e2e suites on AWS (`e2e-aws-operator`, `e2e-aws-olm`, `e2e-aws-operator-components`, `e2e-aws-operator-actuation`).
- **Upgrade tests**: This test should detect any regressions in the operator or operand during an operator upgrade scenario (`e2e-aws-upgrade`).

## Verified Label

Use `/verified` to indicate changes have been verified. Examples:

```text
/verified
```

Mark as verified by referencing specific test coverage:

```text
/verified by unit
/verified by e2e-aws-olm
/verified by E2Es
```

If verification will happen later (e.g., by QE or in a staging environment):

```text
/verified later QE
/verified later @maxcao13
```

## Generated Code

The following files are generated and should never be hand-edited:

| File(s) | Generator | Regenerate with |
|---------|-----------|-----------------|
| `api/v1/zz_generated.deepcopy.go` | controller-gen | `make generate` |
| `config/crd/bases/*.yaml` | controller-gen | `make manifests` |
| `bundle/` | operator-sdk | `make bundle` |
| Parts of `config/manifests/` | operator-sdk | `make bundle` |

After modifying API types, regenerate and commit the results in the same PR. CI runs `make ensure-commands-are-noops` to verify generated output is current.

Operand manifests in `config/vpa/` are maintained manually but must stay in sync with upstream. Use `make manifest-diff` to check.

## Development Quick Reference

| Task | Command |
|------|---------|
| Build operator binary | `make build` |
| Run operator locally | `make run` |
| Run unit tests | `make test` |
| Run linters | `make lint` |
| Run go vet | `make vet` |
| Format code | `make fmt` |
| Generate deepcopy | `make generate` |
| Generate CRD/RBAC manifests | `make manifests` |
| Compare operand manifests to upstream | `make manifest-diff` |
| Lint YAML manifests | `make yamllint` |
| Run all local checks | `make check` |
| Verify generated code is up to date | `make ensure-commands-are-noops` |
| Deploy to cluster (manifests) | `make deploy OPERATOR_IMG=<image>` |
| Generate and validate OLM bundle | `make bundle` |
| Run upstream VPA e2e suite | `make test-e2e SUITE=full-vpa` |
| Run operator e2e suite | `make test-e2e SUITE=operator` |

## Pre-Submit Checklist

Before requesting review:

1. `make build` — Verify the code compiles
2. `make test` — Run unit tests
3. `make lint` — Run linters
4. `make check` — Run manifest-diff, yamllint, lint, and test
5. `make ensure-commands-are-noops` — Ensure generated files are up to date (if API or bundle inputs changed)
6. Review your diff for secrets, credentials, or debug code
7. Address any [CodeRabbit](https://coderabbit.ai/) review feedback before requesting human review. Responding with an explanation of why you are not acting on a suggestion is fine.

## Code Style

- Run `make fmt` before committing
- Follow Go conventions for error strings: lowercase, no trailing punctuation, wrap with `fmt.Errorf("context: %w", err)`
- Use structured logging with logr/klog: constant messages, key-value pairs in lowerCamelCase
- Import ordering: stdlib, external packages, internal packages (separated by blank lines)

## AI Code Review

Our repos use CodeRabbit for automated AI code review. CodeRabbit will post review comments on your PR automatically.
As a courtesy to the human reviewer who follows, please address CodeRabbit’s feedback before requesting human review. You do not need to accept every suggestion — responding with an explanation of why you are not taking action on a comment is perfectly acceptable. The goal is to resolve straightforward issues so that human reviewers can focus on the substantive aspects of the change.
