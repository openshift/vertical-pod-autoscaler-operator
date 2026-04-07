package util

import (
	configv1 "github.com/openshift/api/config/v1"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"
)

// TLSVersionToArg converts an OpenShift TLSProtocolVersion (e.g. "VersionTLS12")
// to the format expected by the VPA admission plugin (e.g. "tls1_2").
func TLSVersionToArg(v configv1.TLSProtocolVersion) string {
	switch v {
	case configv1.VersionTLS10:
		return "tls1_0"
	case configv1.VersionTLS11:
		return "tls1_1"
	case configv1.VersionTLS12:
		return "tls1_2"
	case configv1.VersionTLS13:
		return "tls1_3"
	default:
		return string(v)
	}
}

// TLSCiphersToArgs converts a list of OpenSSL cipher names from an OpenShift
// TLS profile to the IANA names expected by the VPA admission plugin.
func TLSCiphersToArgs(ciphers []string) []string {
	iana := libgocrypto.OpenSSLToIANACipherSuites(ciphers)
	if len(iana) == 0 {
		return ciphers
	}
	return iana
}
