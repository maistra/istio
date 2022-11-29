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
	// golang ciphers
	TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256   = "TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256"   //nolint
	TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256 = "TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256" //nolint
	TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256         = "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"         //nolint
	TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256       = "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256"       //nolint
	TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384         = "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"         //nolint
	TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384       = "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384"       //nolint
	TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256         = "TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256"         //nolint
	TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA            = "TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA"            //nolint
	TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256       = "TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256"       //nolint
	TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA          = "TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA"          //nolint
	TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA            = "TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA"            //nolint
	TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA          = "TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA"          //nolint
	TLS_RSA_WITH_AES_128_GCM_SHA256               = "TLS_RSA_WITH_AES_128_GCM_SHA256"               //nolint
	TLS_RSA_WITH_AES_256_GCM_SHA384               = "TLS_RSA_WITH_AES_256_GCM_SHA384"               //nolint
	TLS_RSA_WITH_AES_128_CBC_SHA256               = "TLS_RSA_WITH_AES_128_CBC_SHA256"               //nolint
	TLS_RSA_WITH_AES_128_CBC_SHA                  = "TLS_RSA_WITH_AES_128_CBC_SHA"                  //nolint
	TLS_RSA_WITH_AES_256_CBC_SHA                  = "TLS_RSA_WITH_AES_256_CBC_SHA"                  //nolint
	TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA           = "TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA"           //nolint
	TLS_RSA_WITH_3DES_EDE_CBC_SHA                 = "TLS_RSA_WITH_3DES_EDE_CBC_SHA"                 //nolint
	// OpenSSL ciphers
	ECDHE_RSA_CHACHA20_POLY1305   = "ECDHE-RSA-CHACHA20-POLY1305"   //nolint
	ECDHE_ECDSA_CHACHA20_POLY1305 = "ECDHE-ECDSA-CHACHA20-POLY1305" //nolint
	ECDHE_RSA_AES128_GCM_SHA256   = "ECDHE-RSA-AES128-GCM-SHA256"   //nolint
	ECDHE_ECDSA_AES128_GCM_SHA256 = "ECDHE-ECDSA-AES128-GCM-SHA256" //nolint
	ECDHE_RSA_AES256_GCM_SHA384   = "ECDHE-RSA-AES256-GCM-SHA384"   //nolint
	ECDHE_ECDSA_AES256_GCM_SHA384 = "ECDHE-ECDSA-AES256-GCM-SHA384" //nolint
	ECDHE_RSA_AES128_SHA256       = "ECDHE-RSA-AES128-SHA256"       //nolint
	ECDHE_RSA_AES128_SHA          = "ECDHE-RSA-AES128-SHA"          //nolint
	ECDHE_ECDSA_AES128_SHA256     = "ECDHE-ECDSA-AES128-SHA256"     //nolint
	ECDHE_ECDSA_AES128_SHA        = "ECDHE-ECDSA-AES128-SHA"        //nolint
	ECDHE_RSA_AES256_SHA          = "ECDHE-RSA-AES256-SHA"          //nolint
	ECDHE_ECDSA_AES256_SHA        = "ECDHE-ECDSA-AES256-SHA"        //nolint
	AES128_GCM_SHA256             = "AES128-GCM-SHA256"             //nolint
	AES256_GCM_SHA384             = "AES256-GCM-SHA384"             //nolint
	AES128_SHA256                 = "AES128-SHA256"                 //nolint
	AES128_SHA                    = "AES128-SHA"                    //nolint
	AES256_SHA                    = "AES256-SHA"                    //nolint
	ECDHE_RSA_DES_CBC3_SHA        = "ECDHE-RSA-DES-CBC3-SHA"        //nolint
	DES_CBC3_SHA                  = "DES-CBC3-SHA"                  //nolint
)

var SupportedGolangCiphers = []string{
	TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
	TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
	TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
	TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256,
	TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
	TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
	TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
	TLS_RSA_WITH_AES_128_GCM_SHA256,
	TLS_RSA_WITH_AES_256_GCM_SHA384,
	TLS_RSA_WITH_AES_128_CBC_SHA256,
	TLS_RSA_WITH_AES_128_CBC_SHA,
	TLS_RSA_WITH_AES_256_CBC_SHA,
	TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA,
	TLS_RSA_WITH_3DES_EDE_CBC_SHA,
}

var SupportedOpenSSLCiphers = []string{
	ECDHE_RSA_CHACHA20_POLY1305,
	ECDHE_ECDSA_CHACHA20_POLY1305,
	ECDHE_RSA_AES128_GCM_SHA256,
	ECDHE_ECDSA_AES128_GCM_SHA256,
	ECDHE_RSA_AES256_GCM_SHA384,
	ECDHE_ECDSA_AES256_GCM_SHA384,
	ECDHE_RSA_AES128_SHA256,
	ECDHE_RSA_AES128_SHA,
	ECDHE_ECDSA_AES128_SHA256,
	ECDHE_ECDSA_AES128_SHA,
	ECDHE_RSA_AES256_SHA,
	ECDHE_ECDSA_AES256_SHA,
	AES128_GCM_SHA256,
	AES256_GCM_SHA384,
	AES128_SHA256,
	AES128_SHA,
	AES256_SHA,
	ECDHE_RSA_DES_CBC3_SHA,
	DES_CBC3_SHA,
}

// Map of go cipher suite names to OpenSSL cipher suite names
var opensslCipherSuiteMap = map[string]string{
	TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256:   ECDHE_RSA_CHACHA20_POLY1305,
	TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256: ECDHE_ECDSA_CHACHA20_POLY1305,
	TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256:         ECDHE_RSA_AES128_GCM_SHA256,
	TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256:       ECDHE_ECDSA_AES128_GCM_SHA256,
	TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384:         ECDHE_RSA_AES256_GCM_SHA384,
	TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384:       ECDHE_ECDSA_AES256_GCM_SHA384,
	TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256:         ECDHE_RSA_AES128_SHA256,
	TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA:            ECDHE_RSA_AES128_SHA,
	TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256:       ECDHE_ECDSA_AES128_SHA256,
	TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA:          ECDHE_ECDSA_AES128_SHA,
	TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA:            ECDHE_RSA_AES256_SHA,
	TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA:          ECDHE_ECDSA_AES256_SHA,
	TLS_RSA_WITH_AES_128_GCM_SHA256:               AES128_GCM_SHA256,
	TLS_RSA_WITH_AES_256_GCM_SHA384:               AES256_GCM_SHA384,
	TLS_RSA_WITH_AES_128_CBC_SHA256:               AES128_SHA256,
	TLS_RSA_WITH_AES_128_CBC_SHA:                  AES128_SHA,
	TLS_RSA_WITH_AES_256_CBC_SHA:                  AES256_SHA,
	TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA:           ECDHE_RSA_DES_CBC3_SHA,
	TLS_RSA_WITH_3DES_EDE_CBC_SHA:                 DES_CBC3_SHA,
}

// // Map of go cipher suite names to go cipher suite ids
var goCipherSuiteIDMap = map[string]uint16{
	TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256:   tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
	TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256: tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
	TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256:         tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256:       tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384:         tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384:       tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256:         tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
	TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA:            tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
	TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256:       tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256,
	TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA:          tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
	TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA:            tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
	TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA:          tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
	TLS_RSA_WITH_AES_128_GCM_SHA256:               tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
	TLS_RSA_WITH_AES_256_GCM_SHA384:               tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
	TLS_RSA_WITH_AES_128_CBC_SHA256:               tls.TLS_RSA_WITH_AES_128_CBC_SHA256,
	TLS_RSA_WITH_AES_128_CBC_SHA:                  tls.TLS_RSA_WITH_AES_128_CBC_SHA,
	TLS_RSA_WITH_AES_256_CBC_SHA:                  tls.TLS_RSA_WITH_AES_256_CBC_SHA,
	TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA:           tls.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA,
	TLS_RSA_WITH_3DES_EDE_CBC_SHA:                 tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA,
}

var (
	TLSCipherSuites = RegisterTLSCipherSuitesVar(
		"TLS_CIPHER_SUITES",
		"",
		"The allowable TLS Cipher suites",
	)

	lock = &sync.Mutex{}
)

type TLSCipherSuitesVar struct {
	env.StringVar
	cipherSuites   []string
	goCipherSuites []uint16
}

func RegisterTLSCipherSuitesVar(name string, defaultValue string, description string) TLSCipherSuitesVar {
	v := env.RegisterStringVar(name, defaultValue, description)
	return TLSCipherSuitesVar{v, nil, nil}
}

func (v *TLSCipherSuitesVar) initCipherSuites() {
	lock.Lock()
	defer lock.Unlock()
	if v.cipherSuites == nil {
		cipherSuitesParam, _ := v.Lookup()
		cipherSuites := []string{}
		goCipherSuites := []string{}
		goCipherSuiteIds := []uint16{}
		if cipherSuitesParam != "" {
			cipherSuitesSlice := strings.Split(cipherSuitesParam, ",")
			for _, cipherSuiteParam := range cipherSuitesSlice {
				trimmed := strings.Trim(cipherSuiteParam, " ")
				cipherSuite := opensslCipherSuiteMap[trimmed]
				goCipherSuite := goCipherSuiteIDMap[trimmed]
				if cipherSuite != "" && goCipherSuite != 0 {
					cipherSuites = append(cipherSuites, cipherSuite)
					goCipherSuites = append(goCipherSuites, trimmed)
					goCipherSuiteIds = append(goCipherSuiteIds, goCipherSuite)
				} else {
					log.Warnf("Cipher %v is not supported, this entry will be ignored", trimmed)
				}
			}
		}
		v.cipherSuites = cipherSuites
		v.goCipherSuites = goCipherSuiteIds
		log.Infof("Go Cipher suites are %v", goCipherSuites)
		log.Infof("OpenSSL Cipher suites are %v", v.cipherSuites)
	}
}

func (v *TLSCipherSuitesVar) Reset() {
	lock.Lock()
	defer lock.Unlock()
	v.cipherSuites = nil
	v.goCipherSuites = nil
}

func (v *TLSCipherSuitesVar) Get() []string {
	if v.cipherSuites == nil {
		v.initCipherSuites()
	}
	if len(v.cipherSuites) == 0 {
		return nil
	}
	result := make([]string, len(v.cipherSuites))
	copy(result, v.cipherSuites)
	return result
}

func (v *TLSCipherSuitesVar) GetGoTLSCipherSuites() []uint16 {
	if v.goCipherSuites == nil {
		v.initCipherSuites()
	}
	if len(v.goCipherSuites) == 0 {
		return nil
	}
	result := make([]uint16, len(v.goCipherSuites))
	copy(result, v.goCipherSuites)
	return result
}
