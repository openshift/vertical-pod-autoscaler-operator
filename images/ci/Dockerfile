FROM registry.ci.openshift.org/ocp/builder:rhel-8-golang-1.20-openshift-4.14 AS builder

WORKDIR /go/src/github.com/openshift/vertical-pod-autoscaler-operator

COPY . .

RUN make build

FROM registry.ci.openshift.org/openshift/origin-v4.0:base

COPY --from=builder /go/src/github.com/openshift/vertical-pod-autoscaler-operator/bin/vertical-pod-autoscaler-operator /usr/bin/

