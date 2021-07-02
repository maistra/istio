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
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/lestrrat-go/jwx/jwk"

	configmemory "istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
)

var trustBundleTestCases = []struct {
	name           string
	certPEMs       []string
	expectedJSON   string
	expectedErrMsg string
}{
	{
		name: "valid",
		certPEMs: []string{`-----BEGIN CERTIFICATE-----
MIIDEzCCAfugAwIBAgIUHu+RNpObSStNAQtgGqFPV70CNVEwDQYJKoZIhvcNAQEL
BQAwGDEWMBQGA1UEAwwNY2x1c3Rlci5sb2NhbDAgFw0yMDAzMTMwMDUwNTBaGA8y
MjkzMTIyNzAwNTA1MFowGDEWMBQGA1UEAwwNY2x1c3Rlci5sb2NhbDCCASIwDQYJ
KoZIhvcNAQEBBQADggEPADCCAQoCggEBAN77xnr0H71SuBEGBI0amTmlvjwcgqZF
mAw5c2pOCs3eUiJSz3tQYTRfVzaWBOrwEmn7RrQH8LJ5EVQm7Prg+aCT7koeBQyD
G/Oqk2hpX1ArCLnjwEildUOlDSg+KBNJiImRFhMb2vnlFPfzkO5TsUk01OOnQTYm
5XMzhQ0CuqX3stcAACsuKGxk3BX3r5zHmcFH4nBpbh9pb0DY2QZ+N+B6Qxe6o89q
bT8gtWYLN1b3yGhn2WGeFkQtaud36Wj2jRSxWo2PBe8xc8qDBFJOt0zHg+u0Pt7t
3mCrBIZ45FWEqX06FOupSe7HF2d7U/9pJGSZLe9iAJ/59WROlniJ7PUCAwEAAaNT
MFEwHQYDVR0OBBYEFC1YD3F9RMLaW1xMuuQN27SmXzsMMB8GA1UdIwQYMBaAFC1Y
D3F9RMLaW1xMuuQN27SmXzsMMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQEL
BQADggEBAGqzLzTnusoOPF+tQ4jbhcXDCMvpDCCiAQVkkaorNGMWg2ny39vm3ymc
HBhqWNNbeyZTj8hl7ffCoeUChZnIFkLO1ev3DVbLz179IP6dhqfDkuLhc/0g5Lfs
FcNedAJh5TConI+x7dH1W2opPX4hXiiJBzcpPmfttD1EYES2VH85cPp95goUJwRp
kMPAS5WiBLPv0c+0Msppe4iqxwuZZtJKRoHRDbvrHonMT1FmdOoEJ8xjeTxB8pjc
tp5dA30dGNAxB7tOrzqmIidjXjYaxZGXKoC6BViw9ISfX9WNqVdvZVMJ7XwL6gu5
Dk0yYXcMuGwMkMz+vJpypIBZAaCFES8=
-----END CERTIFICATE-----`},
		// nolint: lll
		expectedJSON: `{"keys":[{"e":"AQAB","kty":"RSA","n":"3vvGevQfvVK4EQYEjRqZOaW-PByCpkWYDDlzak4Kzd5SIlLPe1BhNF9XNpYE6vASaftGtAfwsnkRVCbs-uD5oJPuSh4FDIMb86qTaGlfUCsIuePASKV1Q6UNKD4oE0mIiZEWExva-eUU9_OQ7lOxSTTU46dBNiblczOFDQK6pfey1wAAKy4obGTcFfevnMeZwUficGluH2lvQNjZBn434HpDF7qjz2ptPyC1Zgs3VvfIaGfZYZ4WRC1q53fpaPaNFLFajY8F7zFzyoMEUk63TMeD67Q-3u3eYKsEhnjkVYSpfToU66lJ7scXZ3tT_2kkZJkt72IAn_n1ZE6WeIns9Q","use":"x509-svid","x5c":["MIIDEzCCAfugAwIBAgIUHu+RNpObSStNAQtgGqFPV70CNVEwDQYJKoZIhvcNAQELBQAwGDEWMBQGA1UEAwwNY2x1c3Rlci5sb2NhbDAgFw0yMDAzMTMwMDUwNTBaGA8yMjkzMTIyNzAwNTA1MFowGDEWMBQGA1UEAwwNY2x1c3Rlci5sb2NhbDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAN77xnr0H71SuBEGBI0amTmlvjwcgqZFmAw5c2pOCs3eUiJSz3tQYTRfVzaWBOrwEmn7RrQH8LJ5EVQm7Prg+aCT7koeBQyDG/Oqk2hpX1ArCLnjwEildUOlDSg+KBNJiImRFhMb2vnlFPfzkO5TsUk01OOnQTYm5XMzhQ0CuqX3stcAACsuKGxk3BX3r5zHmcFH4nBpbh9pb0DY2QZ+N+B6Qxe6o89qbT8gtWYLN1b3yGhn2WGeFkQtaud36Wj2jRSxWo2PBe8xc8qDBFJOt0zHg+u0Pt7t3mCrBIZ45FWEqX06FOupSe7HF2d7U/9pJGSZLe9iAJ/59WROlniJ7PUCAwEAAaNTMFEwHQYDVR0OBBYEFC1YD3F9RMLaW1xMuuQN27SmXzsMMB8GA1UdIwQYMBaAFC1YD3F9RMLaW1xMuuQN27SmXzsMMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEBAGqzLzTnusoOPF+tQ4jbhcXDCMvpDCCiAQVkkaorNGMWg2ny39vm3ymcHBhqWNNbeyZTj8hl7ffCoeUChZnIFkLO1ev3DVbLz179IP6dhqfDkuLhc/0g5LfsFcNedAJh5TConI+x7dH1W2opPX4hXiiJBzcpPmfttD1EYES2VH85cPp95goUJwRpkMPAS5WiBLPv0c+0Msppe4iqxwuZZtJKRoHRDbvrHonMT1FmdOoEJ8xjeTxB8pjctp5dA30dGNAxB7tOrzqmIidjXjYaxZGXKoC6BViw9ISfX9WNqVdvZVMJ7XwL6gu5Dk0yYXcMuGwMkMz+vJpypIBZAaCFES8="]}]}`,
	},
	{
		name: "valid_second",
		certPEMs: []string{`-----BEGIN CERTIFICATE-----
MIIDBTCCAe2gAwIBAgIJALrwrSqYx1KAMA0GCSqGSIb3DQEBCwUAMBgxFjAUBgNV
BAMMDWNsdXN0ZXIubG9jYWwwIBcNMjAwMzEyMTU1OTAxWhgPMjEyMDAyMTcxNTU5
MDFaMBgxFjAUBgNVBAMMDWNsdXN0ZXIubG9jYWwwggEiMA0GCSqGSIb3DQEBAQUA
A4IBDwAwggEKAoIBAQD/DAajjOnqojX3aw38vcDdxNAOmdrjnNdh2U8QWwPBlm50
ZfhANRDnbw06Y3Bs7emRkhtmAbgmxqSWE5OIYKZA9/NjvH71dTDG+uR7CCfDPluC
9Lfq9sgigxiCN/f708QeXnOeElKKanSagd1kfMklSeVd5i0RweFN9BSaJNeMnKu+
1tDG5W0z2onzuthJlVaojaUGNt6woLdfZyB3XHgBSP8c8/kBnzbpi+vw4K6XsdIS
OqI671UFVbDjWr/Rv8vh1oxEKJ/ieXJ0lSbnr3RZ7SvgRUn8sOZbn2kwRA4Xs+1b
odzrAejoy3g7wBKUVQU2zB8ouAD1R9e0FF9lq/dxAgMBAAGjUDBOMB0GA1UdDgQW
BBSdrP1Cx0Al2I38fRzpNJ3ur/H0jzAfBgNVHSMEGDAWgBSdrP1Cx0Al2I38fRzp
NJ3ur/H0jzAMBgNVHRMEBTADAQH/MA0GCSqGSIb3DQEBCwUAA4IBAQBlZjiG9O0w
W7Uy2CDD1P4IwLf7vpPQPJ0iEHV2+KAzEEg8o2HFmmBSU0MjCQuA1ntXw8fTFBR4
5ombL3eR3i5C9RSjg9M+8x7CbAgBo1/jcmMm7/oiX331xQM6r5NhAWHFCdA8T+7L
8Cdfl18p1Ny0C9E05Jv4ow5MNQPcb/xGzLjeosX0hmUxlhTQ7smGOkjgZIeND6Im
9nXVmxjskzUzwq2OmeH/uJ/+nfH7liwZaisdLWSuQlbZGmkRrgGrZX84yfwO7u/q
VOuSLFgc3hNJEONAPfYGlo2o08Tz5BgZGhImDu0VafFI0uaof0UpQ0961k5jCKHh
jLih0+eb5NzN
-----END CERTIFICATE-----`,
			`-----BEGIN CERTIFICATE-----
MIIDEzCCAfugAwIBAgIUHu+RNpObSStNAQtgGqFPV70CNVEwDQYJKoZIhvcNAQEL
BQAwGDEWMBQGA1UEAwwNY2x1c3Rlci5sb2NhbDAgFw0yMDAzMTMwMDUwNTBaGA8y
MjkzMTIyNzAwNTA1MFowGDEWMBQGA1UEAwwNY2x1c3Rlci5sb2NhbDCCASIwDQYJ
KoZIhvcNAQEBBQADggEPADCCAQoCggEBAN77xnr0H71SuBEGBI0amTmlvjwcgqZF
mAw5c2pOCs3eUiJSz3tQYTRfVzaWBOrwEmn7RrQH8LJ5EVQm7Prg+aCT7koeBQyD
G/Oqk2hpX1ArCLnjwEildUOlDSg+KBNJiImRFhMb2vnlFPfzkO5TsUk01OOnQTYm
5XMzhQ0CuqX3stcAACsuKGxk3BX3r5zHmcFH4nBpbh9pb0DY2QZ+N+B6Qxe6o89q
bT8gtWYLN1b3yGhn2WGeFkQtaud36Wj2jRSxWo2PBe8xc8qDBFJOt0zHg+u0Pt7t
3mCrBIZ45FWEqX06FOupSe7HF2d7U/9pJGSZLe9iAJ/59WROlniJ7PUCAwEAAaNT
MFEwHQYDVR0OBBYEFC1YD3F9RMLaW1xMuuQN27SmXzsMMB8GA1UdIwQYMBaAFC1Y
D3F9RMLaW1xMuuQN27SmXzsMMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQEL
BQADggEBAGqzLzTnusoOPF+tQ4jbhcXDCMvpDCCiAQVkkaorNGMWg2ny39vm3ymc
HBhqWNNbeyZTj8hl7ffCoeUChZnIFkLO1ev3DVbLz179IP6dhqfDkuLhc/0g5Lfs
FcNedAJh5TConI+x7dH1W2opPX4hXiiJBzcpPmfttD1EYES2VH85cPp95goUJwRp
kMPAS5WiBLPv0c+0Msppe4iqxwuZZtJKRoHRDbvrHonMT1FmdOoEJ8xjeTxB8pjc
tp5dA30dGNAxB7tOrzqmIidjXjYaxZGXKoC6BViw9ISfX9WNqVdvZVMJ7XwL6gu5
Dk0yYXcMuGwMkMz+vJpypIBZAaCFES8=
-----END CERTIFICATE-----`},
		// nolint: lll
		expectedJSON: `{"keys":[{"e":"AQAB","kty":"RSA","n":"3vvGevQfvVK4EQYEjRqZOaW-PByCpkWYDDlzak4Kzd5SIlLPe1BhNF9XNpYE6vASaftGtAfwsnkRVCbs-uD5oJPuSh4FDIMb86qTaGlfUCsIuePASKV1Q6UNKD4oE0mIiZEWExva-eUU9_OQ7lOxSTTU46dBNiblczOFDQK6pfey1wAAKy4obGTcFfevnMeZwUficGluH2lvQNjZBn434HpDF7qjz2ptPyC1Zgs3VvfIaGfZYZ4WRC1q53fpaPaNFLFajY8F7zFzyoMEUk63TMeD67Q-3u3eYKsEhnjkVYSpfToU66lJ7scXZ3tT_2kkZJkt72IAn_n1ZE6WeIns9Q","use":"x509-svid","x5c":["MIIDEzCCAfugAwIBAgIUHu+RNpObSStNAQtgGqFPV70CNVEwDQYJKoZIhvcNAQELBQAwGDEWMBQGA1UEAwwNY2x1c3Rlci5sb2NhbDAgFw0yMDAzMTMwMDUwNTBaGA8yMjkzMTIyNzAwNTA1MFowGDEWMBQGA1UEAwwNY2x1c3Rlci5sb2NhbDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAN77xnr0H71SuBEGBI0amTmlvjwcgqZFmAw5c2pOCs3eUiJSz3tQYTRfVzaWBOrwEmn7RrQH8LJ5EVQm7Prg+aCT7koeBQyDG/Oqk2hpX1ArCLnjwEildUOlDSg+KBNJiImRFhMb2vnlFPfzkO5TsUk01OOnQTYm5XMzhQ0CuqX3stcAACsuKGxk3BX3r5zHmcFH4nBpbh9pb0DY2QZ+N+B6Qxe6o89qbT8gtWYLN1b3yGhn2WGeFkQtaud36Wj2jRSxWo2PBe8xc8qDBFJOt0zHg+u0Pt7t3mCrBIZ45FWEqX06FOupSe7HF2d7U/9pJGSZLe9iAJ/59WROlniJ7PUCAwEAAaNTMFEwHQYDVR0OBBYEFC1YD3F9RMLaW1xMuuQN27SmXzsMMB8GA1UdIwQYMBaAFC1YD3F9RMLaW1xMuuQN27SmXzsMMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEBAGqzLzTnusoOPF+tQ4jbhcXDCMvpDCCiAQVkkaorNGMWg2ny39vm3ymcHBhqWNNbeyZTj8hl7ffCoeUChZnIFkLO1ev3DVbLz179IP6dhqfDkuLhc/0g5LfsFcNedAJh5TConI+x7dH1W2opPX4hXiiJBzcpPmfttD1EYES2VH85cPp95goUJwRpkMPAS5WiBLPv0c+0Msppe4iqxwuZZtJKRoHRDbvrHonMT1FmdOoEJ8xjeTxB8pjctp5dA30dGNAxB7tOrzqmIidjXjYaxZGXKoC6BViw9ISfX9WNqVdvZVMJ7XwL6gu5Dk0yYXcMuGwMkMz+vJpypIBZAaCFES8="]}]}`,
	},
}

func TestRefreshTrustBundle(t *testing.T) {
	for _, tc := range trustBundleTestCases {
		t.Run(tc.name, func(t *testing.T) {
			currentPEM := -1
			s, _ := NewServer(Options{
				Env:         &model.Environment{},
				BindAddress: "127.0.0.1:0",
				ConfigStore: configmemory.NewController(configmemory.Make(Schemas)),
				GetCARootCertFn: func() string {
					currentPEM++
					return tc.certPEMs[currentPEM]
				},
			})
			for i := 0; i < len(tc.certPEMs); i++ {
				if err := s.refreshTrustBundle(); err != nil {
					t.Error(err)
				}
			}
			bytes, _ := json.Marshal(s.trustBundle)
			if diff := cmp.Diff(string(bytes), tc.expectedJSON); diff != "" {
				t.Errorf("trust bundle does not match: -got +want\n\n%s", diff)
			}
		})
	}
}

func TestHandleTrustBundle(t *testing.T) {
	for _, tc := range trustBundleTestCases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := NewServer(Options{
				Env:         &model.Environment{},
				BindAddress: "127.0.0.1:0",
				ConfigStore: configmemory.NewController(configmemory.Make(Schemas)),
				GetCARootCertFn: func() string {
					return tc.certPEMs[len(tc.certPEMs)-1]
				},
			})
			if err := s.refreshTrustBundle(); err != nil {
				t.Error(err)
			}
			stop := make(chan struct{})
			go s.Run(stop)
			defer close(stop)
			tb := getTrustBundle(t, s.Addr())
			if diff := cmp.Diff(tb, tc.expectedJSON); diff != "" {
				t.Errorf("trust bundle does not match: -got +want\n\n%s", diff)
			}

			certs, err := RetrieveSpiffeBundleRootCerts(
				fmt.Sprintf("http://%s/trust_bundle", s.Addr()),
			)
			if err != nil {
				t.Error(err)
			}
			b := &pem.Block{
				Type:  "CERTIFICATE",
				Bytes: certs[0].Raw,
			}
			var buf bytes.Buffer
			if err = pem.Encode(&buf, b); err != nil {
				t.Error(err)
			}
			if diff := cmp.Diff(strings.Trim(buf.String(), "\n"), tc.certPEMs[len(tc.certPEMs)-1]); diff != "" {
				t.Errorf("trust bundle does not match: -got +want\n\n%s", diff)
			}
		})
	}
}

func getTrustBundle(t *testing.T, addr string) string {
	resp, err := http.Get("http://" + addr + "/trust_bundle")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status code is not OK: %v (%s)", resp.StatusCode, resp.Status)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// adapted from pkg/spiffe/spiffe.go
func RetrieveSpiffeBundleRootCerts(endpoint string) ([]*x509.Certificate, error) {
	httpClient := &http.Client{}
	ret := []*x509.Certificate{}
	httpClient.Transport = &http.Transport{}

	var resp *http.Response
	resp, err := httpClient.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("calling %s failed with error: %v", endpoint, err)
	} else if resp == nil {
		return nil, fmt.Errorf("calling %s failed with nil response", endpoint)
	} else if resp.StatusCode != http.StatusOK {
		b := make([]byte, 1024)
		n, _ := resp.Body.Read(b)
		return nil, fmt.Errorf("calling %s failed with unexpected status: %v, fetching bundle: %s",
			endpoint, resp.StatusCode, string(b[:n]))
	}

	defer resp.Body.Close()
	keySet, err := jwk.ParseReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("URL [%s] failed to decode bundle: %v", endpoint, err)
	}

	var cert *x509.Certificate
	for it := keySet.Iterate(context.Background()); it.Next(context.Background()); {
		key := it.Pair().Value.(jwk.Key)
		if key.KeyUsage() == "x509-svid" {
			if len(key.X509CertChain()) != 1 {
				return nil, fmt.Errorf("expected 1 certificate in x509-svid entry, got %d",
					len(key.X509CertChain()))
			}
			cert = key.X509CertChain()[0]
		}
	}
	if cert == nil {
		return nil, fmt.Errorf("URL [%s] does not provide a X509 SVID", endpoint)
	}
	ret = append(ret, cert)
	return ret, nil
}
