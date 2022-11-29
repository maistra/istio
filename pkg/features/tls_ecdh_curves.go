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
	"strings"
	"sync"

	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

const (
	// golang ECDH Curves
	GoCurveP256 = "CurveP256"
	GoCurveP384 = "CurveP384"
	GoCurveP521 = "CurveP521"
	GoX25519    = "X25519"

	// OpenSSL ECDH Curves
	OpenSSL_P_256  = "P-256"  //nolint
	OpenSSL_P_384  = "P-384"  //nolint
	OpenSSL_P_521  = "P-521"  //nolint
	OpenSSL_X25519 = "X25519" //nolint
)

var SupportedGolangECDHCurves = []string{
	GoCurveP256,
	GoCurveP384,
	GoCurveP521,
	GoX25519,
}

var SupportedOpenSSLECDHCurves = []string{
	OpenSSL_P_256,
	OpenSSL_P_384,
	OpenSSL_P_521,
	OpenSSL_X25519,
}

// Map of go ECDH Curve names to OpenSSL ECDH Curve names
var opensslECDHCurvesMap = map[string]string{
	GoCurveP256: OpenSSL_P_256,
	GoCurveP384: OpenSSL_P_384,
	GoCurveP521: OpenSSL_P_521,
	GoX25519:    OpenSSL_X25519,
}

// Map of go ECDH Curve names to go ECDH Curve ids
var goECDHCurveIDMap = map[string]tls.CurveID{
	GoCurveP256: tls.CurveP256,
	GoCurveP384: tls.CurveP384,
	GoCurveP521: tls.CurveP521,
	GoX25519:    tls.X25519,
}

var (
	TLSECDHCurves = RegisterTLSECDHCurvesVar(
		"TLS_ECDH_CURVES",
		"",
		"The allowable TLS Elliptic Curves",
	)

	eclock = &sync.Mutex{}
)

type TLSECDHCurvesVar struct {
	env.StringVar
	ecdhCurves   []string
	goECDHCurves []tls.CurveID
}

func RegisterTLSECDHCurvesVar(name string, defaultValue string, description string) TLSECDHCurvesVar {
	v := env.RegisterStringVar(name, defaultValue, description)
	return TLSECDHCurvesVar{v, nil, nil}
}

func (v *TLSECDHCurvesVar) initECDHCurves() {
	eclock.Lock()
	defer eclock.Unlock()
	if v.ecdhCurves == nil {
		ecdhCurvesParam, _ := v.Lookup()
		ecdhCurves := []string{}
		goECDHCurves := []string{}
		goECDHCurveIDs := []tls.CurveID{}
		if ecdhCurvesParam != "" {
			ecdhCurvesSlice := strings.Split(ecdhCurvesParam, ",")
			for _, cipherSuiteParam := range ecdhCurvesSlice {
				trimmed := strings.Trim(cipherSuiteParam, " ")
				ecdhCurve := opensslECDHCurvesMap[trimmed]
				goECDHCurve := goECDHCurveIDMap[trimmed]
				if ecdhCurve != "" && goECDHCurve != 0 {
					ecdhCurves = append(ecdhCurves, ecdhCurve)
					goECDHCurves = append(goECDHCurves, trimmed)
					goECDHCurveIDs = append(goECDHCurveIDs, goECDHCurve)
				} else {
					log.Warnf("ECDH Curve %v is not supported, this entry will be ignored", trimmed)
				}
			}
		}
		v.ecdhCurves = ecdhCurves
		v.goECDHCurves = goECDHCurveIDs
		log.Infof("Go ECDH Curves are %v", goECDHCurves)
		log.Infof("OpenSSL ECDH Curves are %v", v.ecdhCurves)
	}
}

func (v *TLSECDHCurvesVar) Reset() {
	eclock.Lock()
	defer eclock.Unlock()
	v.ecdhCurves = nil
	v.goECDHCurves = nil
}

func (v *TLSECDHCurvesVar) Get() []string {
	if v.ecdhCurves == nil {
		v.initECDHCurves()
	}
	if len(v.ecdhCurves) == 0 {
		return nil
	}
	result := make([]string, len(v.ecdhCurves))
	copy(result, v.ecdhCurves)
	return result
}

func (v *TLSECDHCurvesVar) GetGoTLSECDHCurves() []tls.CurveID {
	if v.goECDHCurves == nil {
		v.initECDHCurves()
	}
	if len(v.goECDHCurves) == 0 {
		return nil
	}
	result := make([]tls.CurveID, len(v.goECDHCurves))
	copy(result, v.goECDHCurves)
	return result
}
