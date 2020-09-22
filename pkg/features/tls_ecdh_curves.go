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
	OpenSSL_P_256  = "P-256"
	OpenSSL_P_384  = "P-384"
	OpenSSL_P_521  = "P-521"
	OpenSSL_X25519 = "X25519"
)

var SupportedGolangEcdhCurves = []string{
	GoCurveP256,
	GoCurveP384,
	GoCurveP521,
	GoX25519,
}

var SupportedOpenSSLEcdhCurves = []string{
	OpenSSL_P_256,
	OpenSSL_P_384,
	OpenSSL_P_521,
	OpenSSL_X25519,
}

// Map of go ECDH Curve names to OpenSSL ECDH Curve names
var opensslEcdhCurvesMap = map[string]string{
	GoCurveP256: OpenSSL_P_256,
	GoCurveP384: OpenSSL_P_384,
	GoCurveP521: OpenSSL_P_521,
	GoX25519:    OpenSSL_X25519,
}

// Map of go ECDH Curve names to go ECDH Curve ids
var goEcdhCurveIdMap = map[string]tls.CurveID{
	GoCurveP256: tls.CurveP256,
	GoCurveP384: tls.CurveP384,
	GoCurveP521: tls.CurveP521,
	GoX25519:    tls.X25519,
}

var (
	TlsEcdhCurves = RegisterTlsEcdhCurvesVar(
		"TLS_ECDH_CURVES",
		"",
		"The allowable TLS Elliptic Curves",
	)

	eclock = &sync.Mutex{}
)

type TlsEcdhCurvesVar struct {
	env.StringVar
	ecdhCurves   []string
	goEcdhCurves []tls.CurveID
}

func RegisterTlsEcdhCurvesVar(name string, defaultValue string, description string) TlsEcdhCurvesVar {
	v := env.RegisterStringVar(name, defaultValue, description)
	return TlsEcdhCurvesVar{v, nil, nil}
}

func (v *TlsEcdhCurvesVar) initEcdhCurves() {
	eclock.Lock()
	defer eclock.Unlock()
	if v.ecdhCurves == nil {
		ecdhCurvesParam, _ := v.Lookup()
		ecdhCurves := []string{}
		goEcdhCurves := []string{}
		goEcdhCurveIds := []tls.CurveID{}
		if ecdhCurvesParam != "" {
			ecdhCurvesSlice := strings.Split(ecdhCurvesParam, ",")
			for _, cipherSuiteParam := range ecdhCurvesSlice {
				trimmed := strings.Trim(cipherSuiteParam, " ")
				ecdhCurve := opensslEcdhCurvesMap[trimmed]
				goEcdhCurve := goEcdhCurveIdMap[trimmed]
				if ecdhCurve != "" && goEcdhCurve != 0 {
					ecdhCurves = append(ecdhCurves, ecdhCurve)
					goEcdhCurves = append(goEcdhCurves, trimmed)
					goEcdhCurveIds = append(goEcdhCurveIds, goEcdhCurve)
				} else {
					log.Warnf("ECDH Curve %v is not supported, this entry will be ignored", trimmed)
				}
			}
		}
		v.ecdhCurves = ecdhCurves
		v.goEcdhCurves = goEcdhCurveIds
		log.Infof("Go ECDH Curves are %v", goEcdhCurves)
		log.Infof("OpenSSL ECDH Curves are %v", v.ecdhCurves)
	}
}

func (v *TlsEcdhCurvesVar) Reset() {
	lock.Lock()
	defer lock.Unlock()
	v.ecdhCurves = nil
	v.goEcdhCurves = nil
}

func (v *TlsEcdhCurvesVar) Get() []string {
	if v.ecdhCurves == nil {
		v.initEcdhCurves()
	}
	if len(v.ecdhCurves) == 0 {
		return nil
	} else {
		result := make([]string, len(v.ecdhCurves))
		copy(result, v.ecdhCurves)
		return result
	}
}

func (v *TlsEcdhCurvesVar) GetGoTlsEcdhCurves() []tls.CurveID {
	if v.goEcdhCurves == nil {
		v.initEcdhCurves()
	}
	if len(v.goEcdhCurves) == 0 {
		return nil
	} else {
		result := make([]tls.CurveID, len(v.goEcdhCurves))
		copy(result, v.goEcdhCurves)
		return result
	}
}
