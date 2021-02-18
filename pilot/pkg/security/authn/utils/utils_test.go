// Copyright Istio Authors
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
	"strings"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	tls_features "istio.io/istio/pkg/features"
	protovalue "istio.io/istio/pkg/proto"
	"istio.io/istio/pkg/spiffe"
)

func TestBuildInboundFilterChain(t *testing.T) {
	runTestBuildInboundFilterChain(t, &auth.TlsParameters{})
}

func TestTLSProtocolVersionBuildInboundFilterChain(t *testing.T) {
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

func TestTLSCipherSuitesBuildInboundFilterChain(t *testing.T) {
	_ = os.Setenv("TLS_CIPHER_SUITES", strings.Join(tls_features.SupportedGolangCiphers, ", "))
	tls_features.TLSCipherSuites.Reset()

	defer func() {
		_ = os.Unsetenv("TLS_CIPHER_SUITES")
		tls_features.TLSCipherSuites.Reset()
	}()
	runTestBuildInboundFilterChain(t, &auth.TlsParameters{
		CipherSuites: tls_features.SupportedOpenSSLCiphers,
	})
}

func TestTLSCipherSuitesProtocolVersionBuildInboundFilterChain(t *testing.T) {
	_ = os.Setenv("TLS_CIPHER_SUITES", strings.Join(tls_features.SupportedGolangCiphers, ", "))
	tls_features.TLSCipherSuites.Reset()
	_ = os.Setenv("TLS_MIN_PROTOCOL_VERSION", "TLSv1_2")
	_ = os.Setenv("TLS_MAX_PROTOCOL_VERSION", "TLSv1_3")

	defer func() {
		_ = os.Unsetenv("TLS_CIPHER_SUITES")
		tls_features.TLSCipherSuites.Reset()
		_ = os.Unsetenv("TLS_MIN_PROTOCOL_VERSION")
		_ = os.Unsetenv("TLS_MAX_PROTOCOL_VERSION")
	}()
	runTestBuildInboundFilterChain(t, &auth.TlsParameters{
		TlsMinimumProtocolVersion: auth.TlsParameters_TLSv1_2,
		TlsMaximumProtocolVersion: auth.TlsParameters_TLSv1_3,
		CipherSuites:              tls_features.SupportedOpenSSLCiphers,
	})
}

func TestTLSCipherSuitesEcdhCurvesBuildInboundFilterChain(t *testing.T) {
	_ = os.Setenv("TLS_CIPHER_SUITES", strings.Join(tls_features.SupportedGolangCiphers, ", "))
	_ = os.Setenv("TLS_ECDH_CURVES", strings.Join(tls_features.SupportedGolangECDHCurves, ", "))
	tls_features.TLSCipherSuites.Reset()
	tls_features.TLSECDHCurves.Reset()

	defer func() {
		_ = os.Unsetenv("TLS_CIPHER_SUITES")
		_ = os.Unsetenv("TLS_ECDH_CURVES")
		tls_features.TLSCipherSuites.Reset()
		tls_features.TLSECDHCurves.Reset()
	}()
	runTestBuildInboundFilterChain(t, &auth.TlsParameters{
		CipherSuites: tls_features.SupportedOpenSSLCiphers,
		EcdhCurves:   tls_features.SupportedOpenSSLECDHCurves,
	})
}

func runTestBuildInboundFilterChain(t *testing.T, tlsParam *auth.TlsParameters) {
	type args struct {
		mTLSMode         model.MutualTLSMode
		sdsUdsPath       string
		node             *model.Proxy
		listenerProtocol networking.ListenerProtocol
		trustDomains     []string
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
			name: "MTLSStrict using SDS",
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
					TLSContext: &auth.DownstreamTlsContext{
						CommonTlsContext: &auth.CommonTlsContext{
							TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
								{
									Name: "default",
									SdsConfig: &core.ConfigSource{
										InitialFetchTimeout: ptypes.DurationProto(0 * time.Second),
										ResourceApiVersion:  core.ApiVersion_V3,
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:             core.ApiConfigSource_GRPC,
												TransportApiVersion: core.ApiVersion_V3,
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
									DefaultValidationContext: &auth.CertificateValidationContext{},
									ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
										Name: "ROOTCA",
										SdsConfig: &core.ConfigSource{
											InitialFetchTimeout: ptypes.DurationProto(0 * time.Second),
											ResourceApiVersion:  core.ApiVersion_V3,
											ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
												ApiConfigSource: &core.ApiConfigSource{
													ApiType:             core.ApiConfigSource_GRPC,
													TransportApiVersion: core.ApiVersion_V3,
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
			name: "MTLSStrict using SDS with local trust domain",
			args: args{
				mTLSMode:   model.MTLSStrict,
				sdsUdsPath: "/tmp/sdsuds.sock",
				node: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
				listenerProtocol: networking.ListenerProtocolHTTP,
				trustDomains:     []string{"cluster.local"},
			},
			want: []networking.FilterChain{
				{
					TLSContext: &auth.DownstreamTlsContext{
						CommonTlsContext: &auth.CommonTlsContext{
							TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
								{
									Name: "default",
									SdsConfig: &core.ConfigSource{
										InitialFetchTimeout: ptypes.DurationProto(0 * time.Second),
										ResourceApiVersion:  core.ApiVersion_V3,
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:             core.ApiConfigSource_GRPC,
												TransportApiVersion: core.ApiVersion_V3,
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
									DefaultValidationContext: &auth.CertificateValidationContext{MatchSubjectAltNames: []*matcher.StringMatcher{
										{MatchPattern: &matcher.StringMatcher_Prefix{Prefix: spiffe.URIPrefix + spiffe.GetTrustDomain() + "/"}},
									}},
									ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
										Name: "ROOTCA",
										SdsConfig: &core.ConfigSource{
											InitialFetchTimeout: ptypes.DurationProto(0 * time.Second),
											ResourceApiVersion:  core.ApiVersion_V3,
											ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
												ApiConfigSource: &core.ApiConfigSource{
													ApiType:             core.ApiConfigSource_GRPC,
													TransportApiVersion: core.ApiVersion_V3,
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
			name: "MTLSStrict using SDS with local trust domain and TLSv2 feature disabled",
			args: args{
				mTLSMode:   model.MTLSStrict,
				sdsUdsPath: "/tmp/sdsuds.sock",
				node: &model.Proxy{
					Metadata: &model.NodeMetadata{},
				},
				listenerProtocol: networking.ListenerProtocolHTTP,
				trustDomains:     []string{"cluster.local"},
			},
			want: []networking.FilterChain{
				{
					TLSContext: &auth.DownstreamTlsContext{
						CommonTlsContext: &auth.CommonTlsContext{
							TlsCertificateSdsSecretConfigs: []*auth.SdsSecretConfig{
								{
									Name: "default",
									SdsConfig: &core.ConfigSource{
										InitialFetchTimeout: ptypes.DurationProto(0 * time.Second),
										ResourceApiVersion:  core.ApiVersion_V3,
										ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
											ApiConfigSource: &core.ApiConfigSource{
												ApiType:             core.ApiConfigSource_GRPC,
												TransportApiVersion: core.ApiVersion_V3,
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
									DefaultValidationContext: &auth.CertificateValidationContext{MatchSubjectAltNames: []*matcher.StringMatcher{
										{MatchPattern: &matcher.StringMatcher_Prefix{Prefix: spiffe.URIPrefix + spiffe.GetTrustDomain() + "/"}},
									}},
									ValidationContextSdsSecretConfig: &auth.SdsSecretConfig{
										Name: "ROOTCA",
										SdsConfig: &core.ConfigSource{
											InitialFetchTimeout: ptypes.DurationProto(0 * time.Second),
											ResourceApiVersion:  core.ApiVersion_V3,
											ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
												ApiConfigSource: &core.ApiConfigSource{
													ApiType:             core.ApiConfigSource_GRPC,
													TransportApiVersion: core.ApiVersion_V3,
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildInboundFilterChain(tt.args.mTLSMode, tt.args.sdsUdsPath, tt.args.node, tt.args.listenerProtocol, tt.args.trustDomains)
			if diff := cmp.Diff(got, tt.want, protocmp.Transform()); diff != "" {
				t.Errorf("BuildInboundFilterChain() = %v", diff)
			}
		})
	}
}
