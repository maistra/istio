// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package monitoring_test

import "istio.io/istio/pkg/monitoring"

var (
	method = monitoring.MustCreateLabel("method")

	receivedBytes = monitoring.NewDistribution(
		"received_bytes_total",
		"Distribution of received bytes by method",
		[]float64{10, 100, 1000, 10000},
		monitoring.WithLabels(method),
		monitoring.WithUnit(monitoring.Bytes),
	)
)

func init() {
	monitoring.MustRegister(receivedBytes)
}

func ExampleNewDistribution() {
	receivedBytes.With(method.Value("/projects/1")).Record(458)
}
