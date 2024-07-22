FROM registry.ci.openshift.org/openshift/release:rhel-9-release-golang-1.22-openshift-4.17 AS builder

WORKDIR /go/src/github.com/openshift/vertical-pod-autoscaler-operator

COPY . .

RUN make build

FROM registry.ci.openshift.org/ocp/4.17:base-rhel9

COPY --from=builder /go/src/github.com/openshift/vertical-pod-autoscaler-operator/bin/vertical-pod-autoscaler-operator /usr/bin/

