package features

import (
	"crypto/tls"
	auth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"istio.io/pkg/env"
)

var (
	TlsMinProtocolVersion = RegisterTlsProtocolVersionVar(
		"TLS_MIN_PROTOCOL_VERSION",
		auth.TlsParameters_TLS_AUTO.String(),
		"The default minimum TLS protocol version",
	)

	TlsMaxProtocolVersion = RegisterTlsProtocolVersionVar(
		"TLS_MAX_PROTOCOL_VERSION",
		auth.TlsParameters_TLS_AUTO.String(),
		"The default maximum TLS protocol version",
	)
)

type tlsProtocolVersionVar struct {
	env.StringVar
}

func RegisterTlsProtocolVersionVar(name string, defaultValue string, description string) tlsProtocolVersionVar {
	v := env.RegisterStringVar(name, defaultValue, description)
	return tlsProtocolVersionVar{v}
}

func (v tlsProtocolVersionVar) Get() auth.TlsParameters_TlsProtocol {
	version, _ := v.Lookup()
	return auth.TlsParameters_TlsProtocol(auth.TlsParameters_TlsProtocol_value[version])
}

func GetGoTlsProtocolVersion(tlsVersion auth.TlsParameters_TlsProtocol) uint16 {
	switch tlsVersion {
	case auth.TlsParameters_TLSv1_0:
		return tls.VersionTLS10
	case auth.TlsParameters_TLSv1_1:
		return tls.VersionTLS11
	case auth.TlsParameters_TLSv1_2:
		return tls.VersionTLS12
	case auth.TlsParameters_TLSv1_3:
		return tls.VersionTLS13
	default:
		return 0
	}
}