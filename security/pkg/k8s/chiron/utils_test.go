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
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	cert "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
	pkiutil "istio.io/istio/security/pkg/pki/util"
)

const (
	// exampleCACert copied from samples/certs/ca-cert.pem
	exampleCACert = `-----BEGIN CERTIFICATE-----
MIIDnzCCAoegAwIBAgIJAON1ifrBZ2/BMA0GCSqGSIb3DQEBCwUAMIGLMQswCQYD
VQQGEwJVUzETMBEGA1UECAwKQ2FsaWZvcm5pYTESMBAGA1UEBwwJU3Vubnl2YWxl
MQ4wDAYDVQQKDAVJc3RpbzENMAsGA1UECwwEVGVzdDEQMA4GA1UEAwwHUm9vdCBD
QTEiMCAGCSqGSIb3DQEJARYTdGVzdHJvb3RjYUBpc3Rpby5pbzAgFw0xODAxMjQx
OTE1NTFaGA8yMTE3MTIzMTE5MTU1MVowWTELMAkGA1UEBhMCVVMxEzARBgNVBAgT
CkNhbGlmb3JuaWExEjAQBgNVBAcTCVN1bm55dmFsZTEOMAwGA1UEChMFSXN0aW8x
ETAPBgNVBAMTCElzdGlvIENBMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKC
AQEAyzCxr/xu0zy5rVBiso9ffgl00bRKvB/HF4AX9/ytmZ6Hqsy13XIQk8/u/By9
iCvVwXIMvyT0CbiJq/aPEj5mJUy0lzbrUs13oneXqrPXf7ir3HzdRw+SBhXlsh9z
APZJXcF93DJU3GabPKwBvGJ0IVMJPIFCuDIPwW4kFAI7R/8A5LSdPrFx6EyMXl7K
M8jekC0y9DnTj83/fY72WcWX7YTpgZeBHAeeQOPTZ2KYbFal2gLsar69PgFS0Tom
ESO9M14Yit7mzB1WDK2z9g3r+zLxENdJ5JG/ZskKe+TO4Diqi5OJt/h8yspS1ck8
LJtCole9919umByg5oruflqIlQIDAQABozUwMzALBgNVHQ8EBAMCAgQwDAYDVR0T
BAUwAwEB/zAWBgNVHREEDzANggtjYS5pc3Rpby5pbzANBgkqhkiG9w0BAQsFAAOC
AQEAltHEhhyAsve4K4bLgBXtHwWzo6SpFzdAfXpLShpOJNtQNERb3qg6iUGQdY+w
A2BpmSkKr3Rw/6ClP5+cCG7fGocPaZh+c+4Nxm9suMuZBZCtNOeYOMIfvCPcCS+8
PQ/0hC4/0J3WJKzGBssaaMufJxzgFPPtDJ998kY8rlROghdSaVt423/jXIAYnP3Y
05n8TGERBj7TLdtIVbtUIx3JHAo3PWJywA6mEDovFMJhJERp9sDHIr1BbhXK1TFN
Z6HNH6gInkSSMtvC4Ptejb749PTaePRPF7ID//eq/3AH8UK50F3TQcLjEqWUsJUn
aFKltOc+RAjzDklcUPeG4Y6eMA==
-----END CERTIFICATE-----`

	// exampleIssuedCert copied from samples/certs/cert-chain.pem
	exampleIssuedCert = `-----BEGIN CERTIFICATE-----
MIIDnzCCAoegAwIBAgIJAON1ifrBZ2/BMA0GCSqGSIb3DQEBCwUAMIGLMQswCQYD
VQQGEwJVUzETMBEGA1UECAwKQ2FsaWZvcm5pYTESMBAGA1UEBwwJU3Vubnl2YWxl
MQ4wDAYDVQQKDAVJc3RpbzENMAsGA1UECwwEVGVzdDEQMA4GA1UEAwwHUm9vdCBD
QTEiMCAGCSqGSIb3DQEJARYTdGVzdHJvb3RjYUBpc3Rpby5pbzAgFw0xODAxMjQx
OTE1NTFaGA8yMTE3MTIzMTE5MTU1MVowWTELMAkGA1UEBhMCVVMxEzARBgNVBAgT
CkNhbGlmb3JuaWExEjAQBgNVBAcTCVN1bm55dmFsZTEOMAwGA1UEChMFSXN0aW8x
ETAPBgNVBAMTCElzdGlvIENBMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKC
AQEAyzCxr/xu0zy5rVBiso9ffgl00bRKvB/HF4AX9/ytmZ6Hqsy13XIQk8/u/By9
iCvVwXIMvyT0CbiJq/aPEj5mJUy0lzbrUs13oneXqrPXf7ir3HzdRw+SBhXlsh9z
APZJXcF93DJU3GabPKwBvGJ0IVMJPIFCuDIPwW4kFAI7R/8A5LSdPrFx6EyMXl7K
M8jekC0y9DnTj83/fY72WcWX7YTpgZeBHAeeQOPTZ2KYbFal2gLsar69PgFS0Tom
ESO9M14Yit7mzB1WDK2z9g3r+zLxENdJ5JG/ZskKe+TO4Diqi5OJt/h8yspS1ck8
LJtCole9919umByg5oruflqIlQIDAQABozUwMzALBgNVHQ8EBAMCAgQwDAYDVR0T
BAUwAwEB/zAWBgNVHREEDzANggtjYS5pc3Rpby5pbzANBgkqhkiG9w0BAQsFAAOC
AQEAltHEhhyAsve4K4bLgBXtHwWzo6SpFzdAfXpLShpOJNtQNERb3qg6iUGQdY+w
A2BpmSkKr3Rw/6ClP5+cCG7fGocPaZh+c+4Nxm9suMuZBZCtNOeYOMIfvCPcCS+8
PQ/0hC4/0J3WJKzGBssaaMufJxzgFPPtDJ998kY8rlROghdSaVt423/jXIAYnP3Y
05n8TGERBj7TLdtIVbtUIx3JHAo3PWJywA6mEDovFMJhJERp9sDHIr1BbhXK1TFN
Z6HNH6gInkSSMtvC4Ptejb749PTaePRPF7ID//eq/3AH8UK50F3TQcLjEqWUsJUn
aFKltOc+RAjzDklcUPeG4Y6eMA==
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

func defaultListReactionFunc(obj runtime.Object) kt.ReactionFunc {
	return func(act kt.Action) (bool, runtime.Object, error) {
		return true, &cert.CertificateSigningRequestList{
			Items: []cert.CertificateSigningRequest{*(obj.(*cert.CertificateSigningRequest))},
		}, nil
	}
}

func TestGenKeyCertK8sCA(t *testing.T) {
	testCases := map[string]struct {
		gracePeriodRatio float32
		minGracePeriod   time.Duration
		k8sCaCertFile    string
		dnsNames         []string
		secretNames      []string
		secretNamespace  string
		expectFail       bool
	}{
		"gen cert should succeed": {
			gracePeriodRatio: 0.6,
			k8sCaCertFile:    "./test-data/example-ca-cert.pem",
			dnsNames:         []string{"foo"},
			secretNames:      []string{"istio.webhook.foo"},
			secretNamespace:  "foo.ns",
			expectFail:       false,
		},
	}

	for _, tc := range testCases {
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

		wc, err := NewWebhookController(tc.gracePeriodRatio, tc.minGracePeriod,
			client,
			tc.k8sCaCertFile, tc.secretNames, tc.dnsNames, tc.secretNamespace, "test-issuer")
		if err != nil {
			t.Errorf("failed at creating webhook controller: %v", err)
			continue
		}

		_, _, _, err = GenKeyCertK8sCA(wc.clientset, tc.dnsNames[0], tc.secretNames[0],
			tc.secretNamespace, wc.k8sCaCertFile, "testSigner", true, DefaulCertTTL)
		if tc.expectFail {
			if err == nil {
				t.Errorf("should have failed")
			}
		} else if err != nil {
			t.Errorf("failed unexpectedly: %v", err)
		}
	}
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
		t.Run(tc.certPath, func(t *testing.T) {
			cert, err := readCACert(tc.certPath)
			if tc.shouldFail {
				if err == nil {
					t.Errorf("should have failed at readCACert()")
				} else {
					// Should fail, skip the current case.
					return
				}
			} else if err != nil {
				t.Errorf("failed at readCACert(): %v", err)
			}

			if !bytes.Equal(tc.expectedCert, cert) {
				t.Error("the certificate read is unexpected")
			}
		})
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

func TestReloadCACert(t *testing.T) {
	testCases := map[string]struct {
		gracePeriodRatio float32
		minGracePeriod   time.Duration
		k8sCaCertFile    string
		dnsNames         []string
		secretNames      []string
		secretNamespace  string

		expectFail    bool
		expectChanged bool
	}{
		"reload from valid CA cert path": {
			gracePeriodRatio: 0.6,
			dnsNames:         []string{"foo"},
			secretNames:      []string{"istio.webhook.foo"},
			secretNamespace:  "foo.ns",
			k8sCaCertFile:    "./test-data/example-ca-cert.pem",
			expectFail:       false,
			expectChanged:    false,
		},
	}

	for _, tc := range testCases {
		client := fake.NewSimpleClientset()
		wc, err := NewWebhookController(tc.gracePeriodRatio, tc.minGracePeriod,
			client, tc.k8sCaCertFile, tc.secretNames, tc.dnsNames, tc.secretNamespace, "test-issuer")
		if err != nil {
			t.Errorf("failed at creating webhook controller: %v", err)
			continue
		}
		changed, err := reloadCACert(wc)
		if tc.expectFail {
			if err == nil {
				t.Errorf("should have failed at reloading CA cert")
			}
			continue
		} else if err != nil {
			t.Errorf("failed at reloading CA cert: %v", err)
			continue
		}
		if tc.expectChanged {
			if !changed {
				t.Error("expect changed but not changed")
			}
		} else {
			if changed {
				t.Error("expect unchanged but changed")
			}
		}
	}
}

func TestCheckDuplicateCSR(t *testing.T) {
	testCases := map[string]struct {
		gracePeriodRatio  float32
		minGracePeriod    time.Duration
		k8sCaCertFile     string
		dnsNames          []string
		secretNames       []string
		serviceNamespaces []string
		csrName           string
		secretName        string
		secretNameSpace   string
		expectFail        bool
		isDuplicate       bool
	}{
		"fetching a CSR without a duplicate should fail": {
			gracePeriodRatio:  0.6,
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			dnsNames:          []string{"foo"},
			secretNames:       []string{"istio.webhook.foo"},
			serviceNamespaces: []string{"foo.ns"},
			secretName:        "mock-secret",
			secretNameSpace:   "mock-secret-namespace",
			expectFail:        true,
			csrName:           "domain-cluster.local-ns--secret-mock-secret",
			isDuplicate:       false,
		},
		"fetching a CSR with a duplicate should pass": {
			gracePeriodRatio:  0.6,
			k8sCaCertFile:     "./test-data/example-ca-cert.pem",
			dnsNames:          []string{"foo"},
			secretNames:       []string{"istio.webhook.foo"},
			serviceNamespaces: []string{"foo.ns"},
			secretName:        "mock-secret",
			secretNameSpace:   "mock-secret-namespace",
			expectFail:        false,
			csrName:           "domain-cluster.local-ns--secret-mock-secret",
			isDuplicate:       true,
		},
	}

	for tcName, tc := range testCases {
		client := fake.NewSimpleClientset()
		csr := &cert.CertificateSigningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: tc.csrName,
			},
			Status: cert.CertificateSigningRequestStatus{
				Certificate: []byte(exampleIssuedCert),
			},
		}
		if tc.isDuplicate {
			client.PrependReactor("get", "certificatesigningrequests", defaultReactionFunc(csr))
			client.PrependReactor("list", "certificatesigningrequests", defaultListReactionFunc(csr))
		}
		v1CsrReq, _ := checkDuplicateCsr(client, tc.csrName)
		if tc.expectFail {
			if v1CsrReq != nil {
				t.Errorf("test case (%s) should have failed", tcName)
			}
		} else if v1CsrReq == nil {
			t.Errorf("test case (%s) failed unexpectedly", tcName)
		}

		certData := readSignedCsr(client, tc.csrName, 1*time.Millisecond, 1*time.Millisecond, 1, true)
		if tc.expectFail {
			if len(certData) != 0 {
				t.Errorf("test case (%s) should have failed", tcName)
			}
		} else if len(certData) == 0 {
			t.Errorf("test case (%s) failed unexpectedly", tcName)
		}
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

		wc, err := NewWebhookController(tc.gracePeriodRatio, tc.minGracePeriod,
			client,
			tc.k8sCaCertFile, tc.secretNames, tc.dnsNames, tc.secretNameSpace, "test-issuer")
		if err != nil {
			t.Errorf("test case (%s) failed at creating webhook controller: %v", tcName, err)
			continue
		}

		numRetries := 3

		usages := []cert.KeyUsage{
			cert.UsageDigitalSignature,
			cert.UsageKeyEncipherment,
			cert.UsageServerAuth,
			cert.UsageClientAuth,
		}

		_, r, _, err := submitCSR(wc.clientset, []byte("test-pem"), "test-signer",
			usages, numRetries, DefaulCertTTL)
		if tc.expectFail {
			if err == nil {
				t.Errorf("test case (%s) should have failed", tcName)
			}
		} else if err != nil || r == nil {
			t.Errorf("test case (%s) failed unexpectedly: %v", tcName, err)
		}
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
			client := initFakeKubeClient(t, tc.certificateData)

			wc, err := NewWebhookController(tc.gracePeriodRatio, tc.minGracePeriod,
				client, tc.k8sCaCertFile, tc.secretNames, tc.dnsNames, tc.secretNameSpace, "test-issuer")
			if err != nil {
				t.Fatalf("failed at creating webhook controller: %v", err)
			}

			// 4. Read the signed certificate
			_, _, err = SignCSRK8s(wc.clientset, createFakeCsr(t), "fake-signer", []cert.KeyUsage{cert.UsageAny}, "fake.com",
				wc.k8sCaCertFile, true, true, 1*time.Second)

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

func initFakeKubeClient(t test.Failer, certificate []byte) kube.ExtendedClient {
	client := kube.NewFakeClient()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	w, _ := client.CertificatesV1().CertificateSigningRequests().Watch(ctx, metav1.ListOptions{})
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case r := <-w.ResultChan():
				csr := r.Object.(*cert.CertificateSigningRequest).DeepCopy()
				if csr.Status.Certificate != nil {
					continue
				}
				csr.Status.Certificate = certificate
				client.CertificatesV1().CertificateSigningRequests().UpdateStatus(ctx, csr, metav1.UpdateOptions{})
			}
		}
	}()
	return client
}
