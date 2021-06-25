module github.com/openshift/vertical-pod-autoscaler-operator

go 1.15

require (
	github.com/blang/semver v3.5.1+incompatible
	github.com/go-logr/logr v0.4.0 // indirect
	github.com/openshift/api v0.0.0-20210105115604-44119421ec6b
	github.com/openshift/client-go v0.0.0-20210112165513-ebc401615f47
	github.com/openshift/cluster-version-operator v3.11.1-0.20190615031553-b0250fa556f6+incompatible
	github.com/stretchr/testify v1.6.1
	k8s.io/api v0.20.4
	k8s.io/apimachinery v0.20.5
	k8s.io/client-go v0.20.0
	k8s.io/klog v1.0.0
	k8s.io/kube-aggregator v0.20.0 // indirect
	sigs.k8s.io/controller-runtime v0.6.5
	sigs.k8s.io/yaml v1.2.0
)

replace k8s.io/apiextensions-apiserver v0.0.0-20190315093550-53c4693659ed => github.com/openshift/kubernetes-apiextensions-apiserver v0.0.0-20190315093550-53c4693659ed
