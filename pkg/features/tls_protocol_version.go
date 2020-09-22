// Copyright 2020 Red Hat, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package features

import (
	"crypto/tls"

	auth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"

	"istio.io/pkg/env"
)

var (
	TLSMinProtocolVersion = RegisterTLSProtocolVersionVar(
		"TLS_MIN_PROTOCOL_VERSION",
		auth.TlsParameters_TLS_AUTO.String(),
		"The default minimum TLS protocol version",
	)

	TLSMaxProtocolVersion = RegisterTLSProtocolVersionVar(
		"TLS_MAX_PROTOCOL_VERSION",
		auth.TlsParameters_TLS_AUTO.String(),
		"The default maximum TLS protocol version",
	)
)

type TLSProtocolVersionVar struct {
	env.StringVar
}

func RegisterTLSProtocolVersionVar(name string, defaultValue string, description string) TLSProtocolVersionVar {
	v := env.RegisterStringVar(name, defaultValue, description)
	return TLSProtocolVersionVar{v}
}

func (v *TLSProtocolVersionVar) Get() auth.TlsParameters_TlsProtocol {
	version, _ := v.Lookup()
	return auth.TlsParameters_TlsProtocol(auth.TlsParameters_TlsProtocol_value[version])
}

func (v TLSProtocolVersionVar) GetGoTLSProtocolVersion() uint16 {
	switch v.Get() {
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
