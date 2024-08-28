package verticalpodautoscaler

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// Validator validates VerticalPodAutoscalerController resources.
type Validator struct {
	client  client.Client
	decoder *admission.Decoder
}

// InjectClient injects the client.
func (v *Validator) InjectClient(c client.Client) error {
	v.client = c
	return nil
}

// InjectDecoder injects the decoder.
func (v *Validator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
