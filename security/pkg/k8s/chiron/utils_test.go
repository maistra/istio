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

package chiron

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	cert "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test"
	csrctrl "istio.io/istio/pkg/test/csrctrl/controllers"
	"istio.io/istio/pkg/test/util/assert"
	pkiutil "istio.io/istio/security/pkg/pki/util"
)

const (
	exampleCACert = `-----BEGIN CERTIFICATE-----
MIIDCzCCAfOgAwIBAgIQbfOzhcKTldFipQ1X2WXpHDANBgkqhkiG9w0BAQsFADAv
MS0wKwYDVQQDEyRhNzU5YzcyZC1lNjcyLTQwMzYtYWMzYy1kYzAxMDBmMTVkNWUw
HhcNMTkwNTE2MjIxMTI2WhcNMjQwNTE0MjMxMTI2WjAvMS0wKwYDVQQDEyRhNzU5
YzcyZC1lNjcyLTQwMzYtYWMzYy1kYzAxMDBmMTVkNWUwggEiMA0GCSqGSIb3DQEB
AQUAA4IBDwAwggEKAoIBAQC6sSAN80Ci0DYFpNDumGYoejMQai42g6nSKYS+ekvs
E7uT+eepO74wj8o6nFMNDu58+XgIsvPbWnn+3WtUjJfyiQXxmmTg8om4uY1C7R1H
gMsrL26pUaXZ/lTE8ZV5CnQJ9XilagY4iZKeptuZkxrWgkFBD7tr652EA3hmj+3h
4sTCQ+pBJKG8BJZDNRrCoiABYBMcFLJsaKuGZkJ6KtxhQEO9QxJVaDoSvlCRGa8R
fcVyYQyXOZ+0VHZJQgaLtqGpiQmlFttpCwDiLfMkk3UAd79ovkhN1MCq+O5N7YVt
eVQWaTUqUV2tKUFvVq21Zdl4dRaq+CF5U8uOqLY/4Kg9AgMBAAGjIzAhMA4GA1Ud
DwEB/wQEAwICBDAPBgNVHRMBAf8EBTADAQH/MA0GCSqGSIb3DQEBCwUAA4IBAQCg
oF71Ey2b1QY22C6BXcANF1+wPzxJovFeKYAnUqwh3rF7pIYCS/adZXOKlgDBsbcS
MxAGnCRi1s+A7hMYj3sQAbBXttc31557lRoJrx58IeN5DyshT53t7q4VwCzuCXFT
3zRHVRHQnO6LHgZx1FuKfwtkhfSXDyYU2fQYw2Hcb9krYU/alViVZdE0rENXCClq
xO7AQk5MJcGg6cfE5wWAKU1ATjpK4CN+RTn8v8ODLoI2SW3pfsnXxm93O+pp9HN4
+O+1PQtNUWhCfh+g6BN2mYo2OEZ8qGSxDlMZej4YOdVkW8PHmFZTK0w9iJKqM5o1
V6g5gZlqSoRhICK09tpc
-----END CERTIFICATE-----`

	exampleIssuedCert = `-----BEGIN CERTIFICATE-----
MIIDGDCCAgCgAwIBAgIRAKvYcPLFqnJcwtshCGfNzTswDQYJKoZIhvcNAQELBQAw
LzEtMCsGA1UEAxMkYTc1OWM3MmQtZTY3Mi00MDM2LWFjM2MtZGMwMTAwZjE1ZDVl
MB4XDTE5MDgwNjE5NTU0NVoXDTI0MDgwNDE5NTU0NVowCzEJMAcGA1UEChMAMIIB
IjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAyLIFJU5yJ5VXhbmizir+7Glm
1tVEYXKGiqYbMRbfsFm7V6Z4l00D9/eHvfTXaFpqhv6HBm31MArjYB3OaaV6krvT
whBUEPSkGBFe/eMPSFWBW27a0nw0cK2s/5yuFhTRtcUrZ9+ojJg4IS3oSm2UZ6UJ
DuNI3qwB6OlPQOcWX8uEp4eAaolD1lIbLRQYvxYrBqnyCZBLE+MJgA1/VB3dAECB
TxPtAqcwLFcvsM5ABys8yK8FrqRn5Bx54NiztgG+yU30W33xjdqzmEmuIIk4JjPU
ZQRsug7XClDvQKM6lbYcYS1td2zT08hdgURFXJ9VR64ALFp00/bvglpryu8FmQID
AQABo1MwUTAMBgNVHRMBAf8EAjAAMEEGA1UdEQQ6MDiCHHByb3RvbXV0YXRlLmlz
dGlvLXN5c3RlbS5zdmOCGHByb3RvbXV0YXRlLmlzdGlvLXN5c3RlbTANBgkqhkiG
9w0BAQsFAAOCAQEAhcVEZSuNMqMUJrWVb3b+6pmw9o1f7j6a51KWxOiIl6YuTYFS
WaR0lHSW8wLesjsjm1awWO/F3QRuYWbalANy7434GMAGF53u/uc+Z8aE3EItER9o
SpAJos6OfJqyok7JXDdOYRDD5/hBerj68R9llWzNJd27/1jZ0NF2sIE1W4QFddy/
+8YA4+IqwkWB5/LbeRznl3EjFZDpCEJk0gg5XwAR5eIEy4QU8GueTwrDkssFdBGq
0naco7/Es7CWQscYdKHAgYgk0UAyu8sGV235Uw3hlOrbZ/kqvyUmsSujgT8irmDV
e+5z6MTAO6ktvHdQlSuH6ARn47bJrZOlkttAhg==
-----END CERTIFICATE-----
`
	DefaulCertTTL = 24 * time.Hour
)

type mockTLSServer struct {
	httpServer *httptest.Server
}

func defaultReactionFunc(obj runtime.Object) kt.ReactionFunc {
	return func(act kt.Action) (bool, runtime.Object, error) {
		return true, obj, nil
	}
}

const testSigner = "test-signer"

func runTestSigner(t test.Failer) ([]csrctrl.SignerRootCert, kube.CLIClient) {
	c := kube.NewFakeClient()
	signers, err := csrctrl.RunCSRController(testSigner, test.NewStop(t), []kube.Client{c})
	if err != nil {
		t.Fatal(err)
	}
	return signers, c
}

func TestGenKeyCertK8sCA(t *testing.T) {
	log.FindScope("default").SetOutputLevel(log.DebugLevel)
	signers, client := runTestSigner(t)
	ca := filepath.Join(t.TempDir(), "root-cert.pem")
	os.WriteFile(ca, []byte(signers[0].Rootcert), 0o666)

	_, _, _, err := GenKeyCertK8sCA(client.Kube(), "foo", ca, testSigner, true, DefaulCertTTL)
	assert.NoError(t, err)
}

func TestReadCACert(t *testing.T) {
	testCases := map[string]struct {
		certPath     string
		shouldFail   bool
		expectedCert []byte
	}{
		"cert not exist": {
			certPath:   "./invalid-path/invalid-file",
			shouldFail: true,
		},
		"cert valid": {
			certPath:     "./test-data/example-ca-cert.pem",
			shouldFail:   false,
			expectedCert: []byte(exampleCACert),
		},
		"cert invalid": {
			certPath:   "./test-data/example-invalid-ca-cert.pem",
			shouldFail: true,
		},
	}

	for _, tc := range testCases {
		cert, err := readCACert(tc.certPath)
		if tc.shouldFail {
			if err == nil {
				t.Errorf("should have failed at readCACert()")
			} else {
				// Should fail, skip the current case.
				continue
			}
		} else if err != nil {
			t.Errorf("failed at readCACert(): %v", err)
		}

		if !bytes.Equal(tc.expectedCert, cert) {
			t.Error("the certificate read is unexpected")
		}
	}
}

func TestIsTCPReachable(t *testing.T) {
	server1 := newMockTLSServer(t)
	defer server1.httpServer.Close()
	server2 := newMockTLSServer(t)
	defer server2.httpServer.Close()

	host := "127.0.0.1"
	port1, err := getServerPort(server1.httpServer)
	if err != nil {
		t.Fatalf("error to get the server 1 port: %v", err)
	}
	port2, err := getServerPort(server2.httpServer)
	if err != nil {
		t.Fatalf("error to get the server 2 port: %v", err)
	}

	// Server 1 should be reachable, since it is not closed.
	if !isTCPReachable(host, port1) {
		t.Fatal("server 1 is unreachable")
	}

	// After closing server 2, server 2 should not be reachable
	server2.httpServer.Close()
	if isTCPReachable(host, port2) {
		t.Fatal("server 2 is reachable")
	}
}

func TestSubmitCSR(t *testing.T) {
	testCases := map[string]struct {
		gracePeriodRatio float32
		minGracePeriod   time.Duration
		k8sCaCertFile    string
		dnsNames         []string
		secretNames      []string

		secretName      string
		secretNameSpace string
		expectFail      bool
	}{
		"submitting a CSR without duplicate should succeed": {
			gracePeriodRatio: 0.6,
			k8sCaCertFile:    "./test-data/example-ca-cert.pem",
			dnsNames:         []string{"foo"},
			secretNames:      []string{"istio.webhook.foo"},
			secretName:       "mock-secret",
			secretNameSpace:  "mock-secret-namespace",
			expectFail:       false,
		},
	}

	for tcName, tc := range testCases {
		t.Run(tcName, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			csr := &cert.CertificateSigningRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name: "domain-cluster.local-ns--secret-mock-secret",
				},
				Status: cert.CertificateSigningRequestStatus{
					Certificate: []byte(exampleIssuedCert),
				},
			}
			client.PrependReactor("get", "certificatesigningrequests", defaultReactionFunc(csr))

			usages := []cert.KeyUsage{
				cert.UsageDigitalSignature,
				cert.UsageKeyEncipherment,
				cert.UsageServerAuth,
				cert.UsageClientAuth,
			}
			r, err := submitCSR(client, []byte("test-pem"), "test-signer",
				usages, DefaulCertTTL)
			if tc.expectFail {
				assert.Error(t, err)
			} else if err != nil || r == nil {
				t.Errorf("test case (%s) failed unexpectedly: %v", tcName, err)
			}
		})
	}
}

func TestReadSignedCertificate(t *testing.T) {
	testCases := []struct {
		name              string
		gracePeriodRatio  float32
		minGracePeriod    time.Duration
		k8sCaCertFile     string
		secretNames       []string
		dnsNames          []string
		serviceNamespaces []string

		secretName      string
		secretNameSpace string

		invalidCert     bool
		expectFail      bool
		certificateData []byte
	}{
		{
			name:              "read signed cert should succeed",
			gracePeriodRatio:  0.6,
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			dnsNames:          []string{"foo"},
			secretNames:       []string{"istio.webhook.foo"},
			serviceNamespaces: []string{"foo.ns"},
			secretName:        "mock-secret",
			secretNameSpace:   "mock-secret-namespace",
			invalidCert:       false,
			expectFail:        false,
			certificateData:   []byte(exampleIssuedCert),
		},
		{
			name:              "read invalid signed cert should fail",
			gracePeriodRatio:  0.6,
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			dnsNames:          []string{"foo"},
			secretNames:       []string{"istio.webhook.foo"},
			serviceNamespaces: []string{"foo.ns"},
			secretName:        "mock-secret",
			secretNameSpace:   "mock-secret-namespace",
			invalidCert:       true,
			expectFail:        true,
			certificateData:   []byte("invalid-cert"),
		},
		{
			name:              "read empty signed cert should fail",
			gracePeriodRatio:  0.6,
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			dnsNames:          []string{"foo"},
			secretNames:       []string{"istio.webhook.foo"},
			serviceNamespaces: []string{"foo.ns"},
			secretName:        "mock-secret",
			secretNameSpace:   "mock-secret-namespace",
			invalidCert:       true,
			expectFail:        true,
			certificateData:   []byte(""),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			log.FindScope("default").SetOutputLevel(log.DebugLevel)
			client := initFakeKubeClient(t, tc.certificateData)

			// 4. Read the signed certificate
			_, _, err := SignCSRK8s(client.Kube(), createFakeCsr(t), "fake-signer", []cert.KeyUsage{cert.UsageAny}, "fake.com",
				tc.k8sCaCertFile, true, true, 1*time.Second)

			if tc.expectFail {
				if err == nil {
					t.Fatalf("should have failed at updateMutatingWebhookConfig")
				}
			} else if err != nil {
				t.Fatalf("failed at updateMutatingWebhookConfig: %v", err)
			}
		})
	}
}

func createFakeCsr(t *testing.T) []byte {
	options := pkiutil.CertOptions{
		Host:       "fake.com",
		RSAKeySize: 2048,
		PKCS8Key:   false,
		ECSigAlg:   pkiutil.SupportedECSignatureAlgorithms("ECDSA"),
	}
	csrPEM, _, err := pkiutil.GenCSR(options)
	if err != nil {
		t.Fatalf("Error creating Mock CA client: %v", err)
		return nil
	}
	return csrPEM
}

// newMockTLSServer creates a mock TLS server for testing purpose.
func newMockTLSServer(t *testing.T) *mockTLSServer {
	server := &mockTLSServer{}

	handler := http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		t.Logf("request: %+v", *req)
		switch req.URL.Path {
		default:
			t.Logf("The request contains path: %v", req.URL)
			resp.WriteHeader(http.StatusOK)
		}
	})

	server.httpServer = httptest.NewTLSServer(handler)

	t.Logf("Serving TLS at: %v", server.httpServer.URL)

	return server
}

// Get the server port from server.URL (e.g., https://127.0.0.1:36253)
func getServerPort(server *httptest.Server) (int, error) {
	strs := strings.Split(server.URL, ":")
	if len(strs) < 2 {
		return 0, fmt.Errorf("server.URL is invalid: %v", server.URL)
	}
	port, err := strconv.Atoi(strs[len(strs)-1])
	if err != nil {
		return 0, fmt.Errorf("error to extract port from URL: %v", server.URL)
	}
	return port, nil
}

func initFakeKubeClient(t test.Failer, certificate []byte) kube.CLIClient {
	client := kube.NewFakeClient()
	ctx := test.NewContext(t)
	w, _ := client.Kube().CertificatesV1().CertificateSigningRequests().Watch(ctx, metav1.ListOptions{})
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case r := <-w.ResultChan():
				csr := r.Object.(*cert.CertificateSigningRequest).DeepCopy()
				if csr.Status.Certificate != nil {
					log.Debugf("test signer skip, already signed: %v", csr.Name)
					continue
				}
				if approved(csr) {
					// This is a pretty terrible hack, but client-go fake doesn't properly support list+watch,
					// so any updates in between the list and watch would be missed. So give some time for the watch to start
					time.Sleep(time.Millisecond * 25)
					csr.Status.Certificate = certificate
					_, err := client.Kube().CertificatesV1().CertificateSigningRequests().UpdateStatus(ctx, csr, metav1.UpdateOptions{})
					log.Debugf("test signer sign %v: %v", csr.Name, err)
				} else {
					log.Debugf("test signer skip, not approved: %v", csr.Name)
				}
			}
		}
	}()
	return client
}

func approved(csr *cert.CertificateSigningRequest) bool {
	return GetCondition(csr.Status.Conditions, cert.CertificateApproved).Status == corev1.ConditionTrue
}

func GetCondition(conditions []cert.CertificateSigningRequestCondition, condition cert.RequestConditionType) cert.CertificateSigningRequestCondition {
	for _, cond := range conditions {
		if cond.Type == condition {
			return cond
		}
	}
	return cert.CertificateSigningRequestCondition{}
}
