// Copyright Red Hat, Inc.
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

package server

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"

	"github.com/lestrrat-go/jwx/jwk"
)

type bundleDoc struct {
	Keys        []jwk.Key `json:"keys"`
	Sequence    uint64    `json:"spiffe_sequence,omitempty"`
	RefreshHint int       `json:"spiffe_refresh_hint,omitempty"`
}

func (s *Server) handleTrustBundle(response http.ResponseWriter, request *http.Request) {
	s.RLock()
	defer s.RUnlock()
	respBytes, err := json.Marshal(s.trustBundle)
	if err != nil {
		s.logger.Errorf("failed to marshal to json: %s", err)
		response.WriteHeader(500)
		return
	}
	_, err = response.Write(respBytes)
	if err != nil {
		s.logger.Errorf("failed to send response: %s", err)
		response.WriteHeader(500)
		return
	}
}

func (s *Server) refreshTrustBundle() error {
	s.Lock()
	defer s.Unlock()
	if s.getCARootCert != nil {
		rootCertPEM := s.getCARootCert()
		if rootCertPEM == s.lastSeenRootCertPEM {
			return nil
		}
		s.lastSeenRootCertPEM = rootCertPEM
		block, _ := pem.Decode([]byte(rootCertPEM))
		var cert *x509.Certificate
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return fmt.Errorf("failed to parse root cert to x509: %s", err)
		}
		b := &pem.Block{
			Type:  "CERTIFICATE",
			Bytes: cert.Raw,
		}
		var buf bytes.Buffer
		err = pem.Encode(&buf, b)
		if err != nil {
			return fmt.Errorf("failed to create JWK: %s", err)
		}
		// XXX: do we need to support other key types?
		rsaPublicKey := cert.PublicKey.(*rsa.PublicKey)
		key, err := jwk.New(rsaPublicKey)
		if err != nil {
			return fmt.Errorf("failed to create JWK: %s", err)
		}
		if err = key.Set("use", "x509-svid"); err != nil {
			return fmt.Errorf("failed to create JWK: %s", err)
		}
		if err = key.Set("x5c", base64.StdEncoding.EncodeToString(cert.Raw)); err != nil {
			return fmt.Errorf("failed to create JWK: %s", err)
		}
		if err != nil {
			return fmt.Errorf("failed to create JWK: %s", err)
		}
		if _, ok := key.(jwk.RSAPublicKey); !ok {
			return fmt.Errorf("expected jwk.RSAPublicKey, got %T", key)
		}
		bundle := &bundleDoc{}
		bundle.Keys = []jwk.Key{key}
		s.trustBundle = bundle
	}
	return nil
}
