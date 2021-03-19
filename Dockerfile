FROM registry.ci.openshift.org/openshift/release:golang-1.15 AS builder
WORKDIR /go/src/github.com/openshift/vertical-pod-autoscaler-operator
COPY . .
ENV NO_DOCKER=1
ENV BUILD_DEST=/go/bin/vertical-pod-autoscaler-operator
RUN unset VERSION && make build

FROM registry.ci.openshift.org/openshift/origin-v4.0:base
COPY --from=builder /go/bin/vertical-pod-autoscaler-operator /usr/bin/
COPY --from=builder /go/src/github.com/openshift/vertical-pod-autoscaler-operator/install /manifests
CMD ["/usr/bin/vertical-pod-autoscaler-operator"]
#LABEL io.openshift.release.operator true
