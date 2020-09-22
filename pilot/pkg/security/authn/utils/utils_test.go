// Copyright 2020 Istio Authors
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

package utils

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"
	auth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	structpb "github.com/golang/protobuf/ptypes/struct"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/util"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	tls_features "istio.io/istio/pkg/features"
	protovalue "istio.io/istio/pkg/proto"
)

func TestBuildInboundFilterChain(t *testing.T) {
	runTestBuildInboundFilterChain(t, &auth.TlsParameters{})
}

func TestTlsProtocolVersionBuildInboundFilterChain(t *testing.T) {
	_ = os.Setenv("TLS_MIN_PROTOCOL_VERSION", "TLSv1_2")
	_ = os.Setenv("TLS_MAX_PROTOCOL_VERSION", "TLSv1_3")

	defer func() {
		_ = os.Unsetenv("TLS_MIN_PROTOCOL_VERSION")
		_ = os.Unsetenv("TLS_MAX_PROTOCOL_VERSION")
	}()
	runTestBuildInboundFilterChain(t, &auth.TlsParameters{
		TlsMinimumProtocolVersion: auth.TlsParameters_TLSv1_2,
		TlsMaximumProtocolVersion: auth.TlsParameters_TLSv1_3,
	})
}

func TestTlsCipherSuitesBuildInboundFilterChain(t *testing.T) {
	_ = os.Setenv("TLS_CIPHER_SUITES", strings.Join(tls_features.SupportedGolangCiphers, ", "))
	tls_features.TlsCipherSuites.Reset()

	defer func() {
		_ = os.Unsetenv("TLS_CIPHER_SUITES")
		tls_features.TlsCipherSuites.Reset()
	}()
	runTestBuildInboundFilterChain(t, &auth.TlsParameters{
		CipherSuites: tls_features.SupportedOpenSSLCiphers,
	})
}

func TestTlsCipherSuitesProtocolVersionBuildInboundFilterChain(t *testing.T) {
	_ = os.Setenv("TLS_CIPHER_SUITES", strings.Join(tls_features.SupportedGolangCiphers, ", "))
	tls_features.TlsCipherSuites.Reset()
	_ = os.Setenv("TLS_MIN_PROTOCOL_VERSION", "TLSv1_2")
	_ = os.Setenv("TLS_MAX_PROTOCOL_VERSION", "TLSv1_3")

	defer func() {
		_ = os.Unsetenv("TLS_CIPHER_SUITES")
		tls_features.TlsCipherSuites.Reset()
		_ = os.Unsetenv("TLS_MIN_PROTOCOL_VERSION")
		_ = os.Unsetenv("TLS_MAX_PROTOCOL_VERSION")
	}()
	runTestBuildInboundFilterChain(t, &auth.TlsParameters{
		TlsMinimumProtocolVersion: auth.TlsParameters_TLSv1_2,
		TlsMaximumProtocolVersion: auth.TlsParameters_TLSv1_3,
		CipherSuites:              tls_features.SupportedOpenSSLCiphers,
	})
}

func TestTlsCipherSuitesEcdhCurvesBuildInboundFilterChain(t *testing.T) {
	_ = os.Setenv("TLS_CIPHER_SUITES", strings.Join(tls_features.SupportedGolangCiphers, ", "))
	_ = os.Setenv("TLS_ECDH_CURVES", strings.Join(tls_features.SupportedGolangEcdhCurves, ", "))
	tls_features.TlsCipherSuites.Reset()
	tls_features.TlsEcdhCurves.Reset()

	defer func() {
		_ = os.Unsetenv("TLS_CIPHER_SUITES")
		_ = os.Unsetenv("TLS_ECDH_CURVES")
		tls_features.TlsCipherSuites.Reset()
		tls_features.TlsEcdhCurves.Reset()
	}()
	runTestBuildInboundFilterChain(t, &auth.TlsParameters{
		CipherSuites: tls_features.SupportedOpenSSLCiphers,
		EcdhCurves:   tls_features.SupportedOpenSSLEcdhCurves,
	})
}

func runTestBuildInboundFilterChain(t *testing.T, tlsParam *auth.TlsParameters) {
	tlsContext := func(alpnProtocols []string) *auth.DownstreamTlsContext {
		return &auth.DownstreamTlsContext{
			CommonTlsContext: &auth.CommonTlsContext{
				TlsCertificates: []*auth.TlsCertificate{
					{
						CertificateChain: &core.DataSource{
							Specifier: &core.DataSource_Filename{
								Filename: "/etc/certs/cert-chain.pem",
							},
						},
						PrivateKey: &core.DataSource{
							Specifier: &core.DataSource_Filename{
								Filename: "/etc/certs/key.pem",
							},
						},
					},
				},
				ValidationContextType: &auth.CommonTlsContext_ValidationContext{
					ValidationContext: &auth.CertificateValidationContext{
						TrustedCa: &core.DataSource{
							Specifier: &core.DataSource_Filename{
								Filename: "/etc/certs/root-cert.pem",
							},
						},
					},
				},
				AlpnProtocols: alpnProtocols,
				TlsParams:     tlsParam,
			},
			RequireClientCertificate: protovalue.BoolTrue,
		}
	}

	type args struct {
		mTLSMode         model.MutualTLSMode
		sdsUdsPath       string
		node             *model.Proxy
		listenerProtocol networking.ListenerProtocol
	}
	tests := []struct {
		name string
		args args
		want []networking.FilterChain
	}{
		{
			name: "MTLSUnknown",
			args: args{
				mTLSMode: model.MTLSUnknown,
				node: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
				listenerProtocol: networking.ListenerProtocolAuto,
			},
			// No need to set up filter chain, default one is okay.
			want: nil,
		},
		{
			name: "MTLSDisable",
			args: args{
				mTLSMode: model.MTLSDisable,
				node: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
				listenerProtocol: networking.ListenerProtocolAuto,
			},
			want: nil,
		},
		{
			name: "MTLSStrict",
			args: args{
				mTLSMode: model.MTLSStrict,
				node: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
				listenerProtocol: networking.ListenerProtocolHTTP,
			},
			want: []networking.FilterChain{
				{
					TLSContext: tlsContext([]string{"h2", "http/1.1"}),
				},
			},
		},
		{
			name: "MTLSPermissive",
			args: args{
				mTLSMode: model.MTLSPermissive,
				node: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
				listenerProtocol: networking.ListenerProtocolTCP,
			},
			// Two filter chains, one for mtls traffic within the mesh, one for plain text traffic.
			want: []networking.FilterChain{
				{
					TLSContext: tlsContext([]string{"istio-peer-exchange", "h2", "http/1.1"}),
					FilterChainMatch: &listener.FilterChainMatch{
						ApplicationProtocols: []string{"istio-peer-exchange", "istio"},
					},
					ListenerFilters: []*listener.ListenerFilter{
						{
							Name:       "envoy.listener.tls_inspector",
							ConfigType: &listener.ListenerFilter_Config{&structpb.Struct{}},
						},
					},
				},
				{
					FilterChainMatch: &listener.FilterChainMatch{},
				},
			},
		},
		{
			name: "MTLSStrict using SDS",
			args: args{
				mTLSMode:   model.MTLSStrict,
				sdsUdsPath: "/tmp/sdsuds.sock",
				node: &model.Proxy{
					Metadata: &model.NodeMetadata{
						SdsEnabled: true,
					},
				},
				listenerProtocol: networking.ListenerProtocolHTTP,
			},
			want: []networking.FilterChain{
				{
					TLSContext: &auth.DownstreamTlsContext{
						CommonTlsContext: &auth.CommonTlsContext{
							TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
								{
									Name: "default",
									SdsConfig: &core.ConfigSource{
										InitialFetchTimeout: features.InitialFetchTimeout,
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType: core.ApiConfigSource_GRPC,
												GrpcServices: []*core.GrpcService{
													{
														TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
															EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: authn_model.SDSClusterName},
														},
													},
												},
											},
										},
									},
								},
							},
							ValidationContextType: &auth.CommonTlsContext_CombinedValidationContext{
								CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
									DefaultValidationContext: &auth.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch([]string{})},
									ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
										Name: "ROOTCA",
										SdsConfig: &core.ConfigSource{
											InitialFetchTimeout: features.InitialFetchTimeout,
											ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
												ApiConfigSource: &core.ApiConfigSource{
													ApiType: core.ApiConfigSource_GRPC,
													GrpcServices: []*core.GrpcService{
														{
															TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
																EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: authn_model.SDSClusterName},
															},
														},
													},
												},
											},
										},
									},
								},
							},
							AlpnProtocols: []string{"h2", "http/1.1"},
							TlsParams:     tlsParam,
						},
						RequireClientCertificate: protovalue.BoolTrue,
					},
				},
			},
		},
		{
			name: "MTLSStrict using SDS without node meta",
			args: args{
				mTLSMode:   model.MTLSStrict,
				sdsUdsPath: "/tmp/sdsuds.sock",
				node: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
				listenerProtocol: networking.ListenerProtocolHTTP,
			},
			want: []networking.FilterChain{
				{
					TLSContext: tlsContext([]string{"h2", "http/1.1"}),
				},
			},
		},
		{
			name: "MTLSStrict with custom cert paths from proxy node metadata",
			args: args{
				mTLSMode: model.MTLSStrict,
				node: &model.Proxy{
					Metadata: &model.NodeMetadata{
						TLSServerCertChain: "/custom/path/to/cert-chain.pem",
						TLSServerKey:       "/custom-key.pem",
						TLSServerRootCert:  "/custom/path/to/root.pem",
					},
				},
				listenerProtocol: networking.ListenerProtocolHTTP,
			},
			// Only one filter chain with mTLS settings should be generated.
			want: []networking.FilterChain{
				{
					TLSContext: &auth.DownstreamTlsContext{
						CommonTlsContext: &auth.CommonTlsContext{
							TlsCertificates: []*auth.TlsCertificate{
								{
									CertificateChain: &core.DataSource{
										Specifier: &core.DataSource_Filename{
											Filename: "/custom/path/to/cert-chain.pem",
										},
									},
									PrivateKey: &core.DataSource{
										Specifier: &core.DataSource_Filename{
											Filename: "/custom-key.pem",
										},
									},
								},
							},
							ValidationContextType: &auth.CommonTlsContext_ValidationContext{
								ValidationContext: &auth.CertificateValidationContext{
									TrustedCa: &core.DataSource{
										Specifier: &core.DataSource_Filename{
											Filename: "/custom/path/to/root.pem",
										},
									},
								},
							},
							AlpnProtocols: []string{"h2", "http/1.1"},
							TlsParams:     tlsParam,
						},
						RequireClientCertificate: protovalue.BoolTrue,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildInboundFilterChain(tt.args.mTLSMode, tt.args.sdsUdsPath, tt.args.node, tt.args.listenerProtocol); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("BuildInboundFilterChain() = %v, want %v", spew.Sdump(got), spew.Sdump(tt.want))
				if len(got[0].TLSContext.CommonTlsContext.TlsCertificateSdsSecretConfigs) > 0 {
					t.Logf("got:\n%v\n", got[0].TLSContext.CommonTlsContext.TlsCertificateSdsSecretConfigs[0])
				} else {
					t.Logf("no secrets returned, got: \n%v\n", got[0].TLSContext.CommonTlsContext)
				}
			}
		})
	}
}
