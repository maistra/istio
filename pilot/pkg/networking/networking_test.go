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

package networking

import (
	"testing"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

	"istio.io/istio/pkg/config/protocol"
)

func TestModelProtocolToListenerProtocol(t *testing.T) {
	tests := []struct {
		name      string
		protocol  protocol.Instance
		direction core.TrafficDirection
		want      ListenerProtocol
	}{
		{
			"TCP to TCP",
			protocol.TCP,
			core.TrafficDirection_INBOUND,
			ListenerProtocolTCP,
		},
		{
			"HTTP to HTTP",
			protocol.HTTP,
			core.TrafficDirection_INBOUND,
			ListenerProtocolHTTP,
		},
		{
			"HTTP to HTTP",
			protocol.HTTP_PROXY,
			core.TrafficDirection_OUTBOUND,
			ListenerProtocolHTTP,
		},
		{
			"MySQL to TCP",
			protocol.MySQL,
			core.TrafficDirection_INBOUND,
			ListenerProtocolTCP,
		},
		{
			"Inbound unknown to Auto",
			protocol.Unsupported,
			core.TrafficDirection_INBOUND,
			ListenerProtocolAuto,
		},
		{
			"Outbound unknown to Auto",
			protocol.Unsupported,
			core.TrafficDirection_OUTBOUND,
			ListenerProtocolAuto,
		},
		{
			"UDP to UDP",
			protocol.UDP,
			core.TrafficDirection_INBOUND,
			ListenerProtocolUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ModelProtocolToListenerProtocol(tt.protocol); got != tt.want {
				t.Errorf("ModelProtocolToListenerProtocol() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		name  string
		value uint
		want  string
	}{
		{
			"test String method for tcp transport protocol",
			TransportProtocolTCP,
			"tcp",
		},
		{
			"test String method for quic transport protocol",
			TransportProtocolQUIC,
			"quic",
		},
		{
			"test String method for invalid transport protocol",
			3,
			"unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TransportProtocol(tt.value).String(); got != tt.want {
				t.Errorf("Failed to get TransportProtocol.String :: got = %v, want %v", got, tt.want)
			}
		})
	}
}
