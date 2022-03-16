// Copyright 2018 Istio Authors
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

package inject

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ghodss/yaml"
	"github.com/howeyc/fsnotify"

	"istio.io/api/label"

	"istio.io/api/annotation"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/cmd/pilot-agent/status"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/mesh"
	tls_features "istio.io/istio/pkg/features"
	"istio.io/istio/pkg/kube"

	"istio.io/pkg/log"

	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

const proxyUIDAnnotation = "sidecar.istio.io/proxyUID"

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

func init() {
	_ = corev1.AddToScheme(runtimeScheme)
	_ = v1beta1.AddToScheme(runtimeScheme)
}

const (
	watchDebounceDelay = 100 * time.Millisecond
)

// Webhook implements a mutating webhook for automatic proxy injection.
type Webhook struct {
	mu                     sync.RWMutex
	Config                 *Config
	sidecarTemplateVersion string
	meshConfig             *meshconfig.MeshConfig
	valuesConfig           string

	healthCheckInterval time.Duration
	healthCheckFile     string

	server     *http.Server
	meshFile   string
	configFile string
	valuesFile string
	watcher    *fsnotify.Watcher
	certFile   string
	keyFile    string
	cert       *tls.Certificate
	mon        *monitor
	env        *model.Environment
	revision   string
}

// env will be used for other things besides meshConfig - when webhook is running in Istiod it can take advantage
// of the config and endpoint cache.
//nolint directives: interfacer
func loadConfig(injectFile, meshFile, valuesFile string, env *model.Environment) (*Config, *meshconfig.MeshConfig, string, error) {
	data, err := ioutil.ReadFile(injectFile)
	if err != nil {
		return nil, nil, "", err
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		log.Warnf("Failed to parse injectFile %s", string(data))
		return nil, nil, "", err
	}

	valuesConfig, err := ioutil.ReadFile(valuesFile)
	if err != nil {
		return nil, nil, "", err
	}

	var meshConfig *meshconfig.MeshConfig
	if env != nil {
		meshConfig = env.Mesh()
	} else {
		meshConfig, err = mesh.ReadMeshConfig(meshFile)
		if err != nil {
			return nil, nil, "", err
		}
	}

	log.Debugf("New inject configuration: sha256sum %x", sha256.Sum256(data))
	log.Debugf("Policy: %v", c.Policy)
	log.Debugf("AlwaysInjectSelector: %v", c.AlwaysInjectSelector)
	log.Debugf("NeverInjectSelector: %v", c.NeverInjectSelector)
	log.Debugf("Template: |\n  %v", strings.Replace(c.Template, "\n", "\n  ", -1))

	return &c, meshConfig, string(valuesConfig), nil
}

// WebhookParameters configures parameters for the sidecar injection
// webhook.
type WebhookParameters struct {
	// ConfigFile is the path to the sidecar injection configuration file.
	ConfigFile string

	ValuesFile string

	// MeshFile is the path to the mesh configuration file.
	MeshFile string

	// CertFile is the path to the x509 certificate for https.
	CertFile string

	// KeyFile is the path to the x509 private key matching `CertFile`.
	KeyFile string

	// Port is the webhook port, e.g. typically 443 for https.
	Port int

	// MonitoringPort is the webhook port, e.g. typically 15014.
	// Set to -1 to disable monitoring
	MonitoringPort int

	// HealthCheckInterval configures how frequently the health check
	// file is updated. Value of zero disables the health check
	// update.
	HealthCheckInterval time.Duration

	// HealthCheckFile specifies the path to the health check file
	// that is periodically updated.
	HealthCheckFile string

	Env *model.Environment

	// Use an existing mux instead of creating our own.
	Mux *http.ServeMux

	// The istio.io/rev this injector is responsible for
	Revision string
}

// NewWebhook creates a new instance of a mutating webhook for automatic sidecar injection.
func NewWebhook(p WebhookParameters) (*Webhook, error) {
	// TODO: pass a pointer to mesh config from Pilot bootstrap, no need to watch and load 2 times
	// This is needed before we implement advanced merging / patching of mesh config
	sidecarConfig, meshConfig, valuesConfig, err := loadConfig(p.ConfigFile, p.MeshFile, p.ValuesFile, p.Env)
	if err != nil {
		return nil, err
	}
	pair, err := tls.LoadX509KeyPair(p.CertFile, p.KeyFile)
	if err != nil {
		return nil, err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	// watch the parent directory of the target files so we can catch
	// symlink updates of k8s ConfigMaps volumes.
	for _, file := range []string{p.ConfigFile, p.MeshFile, p.CertFile, p.KeyFile} {
		if file == p.MeshFile && p.Env != nil {
			continue
		}
		watchDir, _ := filepath.Split(file)
		if err := watcher.Watch(watchDir); err != nil {
			return nil, fmt.Errorf("could not watch %v: %v", file, err)
		}
	}

	wh := &Webhook{
		Config:                 sidecarConfig,
		sidecarTemplateVersion: sidecarTemplateVersionHash(sidecarConfig.Template),
		meshConfig:             meshConfig,
		configFile:             p.ConfigFile,
		valuesFile:             p.ValuesFile,
		valuesConfig:           valuesConfig,
		meshFile:               p.MeshFile,
		watcher:                watcher,
		healthCheckInterval:    p.HealthCheckInterval,
		healthCheckFile:        p.HealthCheckFile,
		certFile:               p.CertFile,
		keyFile:                p.KeyFile,
		cert:                   &pair,
		env:                    p.Env,
		revision:               p.Revision,
	}

	var mux *http.ServeMux
	if p.Mux != nil {
		p.Mux.HandleFunc("/inject", wh.serveInject)
		p.Mux.HandleFunc("/inject/", wh.serveInject)
		mux = p.Mux
	} else {
		wh.server = &http.Server{
			Addr: fmt.Sprintf(":%v", p.Port),
			// mtls disabled because apiserver webhook cert usage is still TBD.
			TLSConfig: &tls.Config{
				GetCertificate:   wh.getCert,
				MinVersion:       tls_features.TLSMinProtocolVersion.GetGoTLSProtocolVersion(),
				MaxVersion:       tls_features.TLSMaxProtocolVersion.GetGoTLSProtocolVersion(),
				CipherSuites:     tls_features.TLSCipherSuites.GetGoTLSCipherSuites(),
				CurvePreferences: tls_features.TLSECDHCurves.GetGoTLSECDHCurves(),
			},
		}
		mux = http.NewServeMux()
		mux.HandleFunc("/inject", wh.serveInject)
		mux.HandleFunc("/inject/", wh.serveInject)
		wh.server.Handler = mux
	}

	if p.Env != nil {
		p.Env.Watcher.AddMeshHandler(func() {
			wh.mu.Lock()
			wh.meshConfig = p.Env.Mesh()
			wh.mu.Unlock()
		})
	}

	if p.MonitoringPort >= 0 {
		mon, err := startMonitor(mux, p.MonitoringPort)
		if err != nil {
			return nil, fmt.Errorf("could not start monitoring server %v", err)
		}
		wh.mon = mon
	}

	return wh, nil
}

// Run implements the webhook server
func (wh *Webhook) Run(stop <-chan struct{}) {
	if wh.server != nil {
		go func() {
			if err := wh.server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				log.Fatalf("admission webhook ListenAndServeTLS failed: %v", err)
			}
		}()
		defer wh.server.Close()
	}
	defer wh.watcher.Close()

	if wh.mon != nil {
		defer wh.mon.monitoringServer.Close()
	}

	var healthC <-chan time.Time
	if wh.healthCheckInterval != 0 && wh.healthCheckFile != "" {
		t := time.NewTicker(wh.healthCheckInterval)
		healthC = t.C
		defer t.Stop()
	}
	var timerC <-chan time.Time

	for {
		select {
		case <-timerC:
			timerC = nil
			sidecarConfig, meshConfig, valuesConfig, err := loadConfig(wh.configFile, wh.meshFile, wh.valuesFile, wh.env)
			if err != nil {
				log.Errorf("update error: %v", err)
				break
			}

			version := sidecarTemplateVersionHash(sidecarConfig.Template)
			pair, err := tls.LoadX509KeyPair(wh.certFile, wh.keyFile)
			if err != nil {
				log.Errorf("reload cert error: %v", err)
				break
			}
			wh.mu.Lock()
			wh.Config = sidecarConfig
			wh.valuesConfig = valuesConfig
			wh.sidecarTemplateVersion = version
			wh.meshConfig = meshConfig
			wh.cert = &pair
			wh.mu.Unlock()
		case event := <-wh.watcher.Event:
			log.Debugf("Injector watch update: %+v", event)
			// use a timer to debounce configuration updates
			if (event.IsModify() || event.IsCreate()) && timerC == nil {
				timerC = time.After(watchDebounceDelay)
			}
		case err := <-wh.watcher.Error:
			log.Errorf("Watcher error: %v", err)
		case <-healthC:
			content := []byte(`ok`)
			if err := ioutil.WriteFile(wh.healthCheckFile, content, 0644); err != nil {
				log.Errorf("Health check update of %q failed: %v", wh.healthCheckFile, err)
			}
		case <-stop:
			return
		}
	}
}

func (wh *Webhook) getCert(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	wh.mu.Lock()
	defer wh.mu.Unlock()
	return wh.cert, nil
}

// It would be great to use https://github.com/mattbaird/jsonpatch to
// generate RFC6902 JSON patches. Unfortunately, it doesn't produce
// correct patches for object removal. Fortunately, our patching needs
// are fairly simple so generating them manually isn't horrible (yet).
type rfc6902PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// JSONPatch `remove` is applied sequentially. Remove items in reverse
// order to avoid renumbering indices.
func removeContainers(containers []corev1.Container, removed []string, path string) (patch []rfc6902PatchOperation) {
	names := map[string]bool{}
	for _, name := range removed {
		names[name] = true
	}
	for i := len(containers) - 1; i >= 0; i-- {
		if _, ok := names[containers[i].Name]; ok {
			patch = append(patch, rfc6902PatchOperation{
				Op:   "remove",
				Path: fmt.Sprintf("%v/%v", path, i),
			})
		}
	}
	return patch
}

func removeVolumes(volumes []corev1.Volume, removed []string, path string) (patch []rfc6902PatchOperation) {
	names := map[string]bool{}
	for _, name := range removed {
		names[name] = true
	}
	for i := len(volumes) - 1; i >= 0; i-- {
		if _, ok := names[volumes[i].Name]; ok {
			patch = append(patch, rfc6902PatchOperation{
				Op:   "remove",
				Path: fmt.Sprintf("%v/%v", path, i),
			})
		}
	}
	return patch
}

func removeImagePullSecrets(imagePullSecrets []corev1.LocalObjectReference, removed []string, path string) (patch []rfc6902PatchOperation) {
	names := map[string]bool{}
	for _, name := range removed {
		names[name] = true
	}
	for i := len(imagePullSecrets) - 1; i >= 0; i-- {
		if _, ok := names[imagePullSecrets[i].Name]; ok {
			patch = append(patch, rfc6902PatchOperation{
				Op:   "remove",
				Path: fmt.Sprintf("%v/%v", path, i),
			})
		}
	}
	return patch
}

func addContainer(target, added []corev1.Container, basePath string) (patch []rfc6902PatchOperation) {
	saJwtSecretMountName := ""
	var saJwtSecretMount corev1.VolumeMount
	// find service account secret volume mount(/var/run/secrets/kubernetes.io/serviceaccount,
	// https://kubernetes.io/docs/reference/access-authn-authz/service-accounts-admin/#service-account-automation) from app container
	for _, add := range target {
		for _, vmount := range add.VolumeMounts {
			if vmount.MountPath == "/var/run/secrets/kubernetes.io/serviceaccount" {
				saJwtSecretMountName = vmount.Name
				saJwtSecretMount = vmount
			}
		}
	}
	first := len(target) == 0
	var value interface{}
	for _, add := range added {
		if add.Name == sidecarContainerName && saJwtSecretMountName != "" {
			// add service account secret volume mount(/var/run/secrets/kubernetes.io/serviceaccount,
			// https://kubernetes.io/docs/reference/access-authn-authz/service-accounts-admin/#service-account-automation) to istio-proxy container,
			// so that envoy could fetch/pass k8s sa jwt and pass to sds server, which will be used to request workload identity for the pod.
			add.VolumeMounts = append(add.VolumeMounts, saJwtSecretMount)
		}
		value = add
		path := basePath
		if first {
			first = false
			value = []corev1.Container{add}
		} else if add.Name == "istio-validation" {
			path += "/0"
		} else {
			path += "/-"
		}
		patch = append(patch, rfc6902PatchOperation{
			Op:    "add",
			Path:  path,
			Value: value,
		})
	}
	return patch
}

func addSecurityContext(target *corev1.PodSecurityContext, basePath string) (patch []rfc6902PatchOperation) {
	patch = append(patch, rfc6902PatchOperation{
		Op:    "add",
		Path:  basePath,
		Value: target,
	})
	return patch
}

func addVolume(target, added []corev1.Volume, basePath string) (patch []rfc6902PatchOperation) {
	first := len(target) == 0
	var value interface{}
	for _, add := range added {
		value = add
		path := basePath
		if first {
			first = false
			value = []corev1.Volume{add}
		} else {
			path += "/-"
		}
		patch = append(patch, rfc6902PatchOperation{
			Op:    "add",
			Path:  path,
			Value: value,
		})
	}
	return patch
}

func addImagePullSecrets(target, added []corev1.LocalObjectReference, basePath string) (patch []rfc6902PatchOperation) {
	first := len(target) == 0
	var value interface{}
	for _, add := range added {
		value = add
		path := basePath
		if first {
			first = false
			value = []corev1.LocalObjectReference{add}
		} else {
			path += "/-"
		}
		patch = append(patch, rfc6902PatchOperation{
			Op:    "add",
			Path:  path,
			Value: value,
		})
	}
	return patch
}

func addPodDNSConfig(target *corev1.PodDNSConfig, basePath string) (patch []rfc6902PatchOperation) {
	patch = append(patch, rfc6902PatchOperation{
		Op:    "add",
		Path:  basePath,
		Value: target,
	})
	return patch
}

// escape JSON Pointer value per https://tools.ietf.org/html/rfc6901
func escapeJSONPointerValue(in string) string {
	step := strings.Replace(in, "~", "~0", -1)
	return strings.Replace(step, "/", "~1", -1)
}

// adds labels to the target spec, will not overwrite label's value if it already exists
func addLabels(target map[string]string, added map[string]string) []rfc6902PatchOperation {
	patches := []rfc6902PatchOperation{}

	addedKeys := make([]string, 0, len(added))
	for key := range added {
		addedKeys = append(addedKeys, key)
	}
	sort.Strings(addedKeys)

	for _, key := range addedKeys {
		value := added[key]
		patch := rfc6902PatchOperation{
			Op:    "add",
			Path:  "/metadata/labels/" + escapeJSONPointerValue(key),
			Value: value,
		}

		if target == nil {
			target = map[string]string{}
			patch.Path = "/metadata/labels"
			patch.Value = map[string]string{
				key: value,
			}
		}

		if target[key] == "" {
			patches = append(patches, patch)
		}
	}

	return patches
}

func updateAnnotation(target map[string]string, added map[string]string) (patch []rfc6902PatchOperation) {
	// To ensure deterministic patches, we sort the keys
	var keys []string
	for k := range added {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := added[key]
		if target == nil {
			target = map[string]string{}
			patch = append(patch, rfc6902PatchOperation{
				Op:   "add",
				Path: "/metadata/annotations",
				Value: map[string]string{
					key: value,
				},
			})
		} else {
			op := "add"
			if target[key] != "" {
				op = "replace"
			}
			patch = append(patch, rfc6902PatchOperation{
				Op:    op,
				Path:  "/metadata/annotations/" + escapeJSONPointerValue(key),
				Value: value,
			})
		}
	}
	return patch
}

func createPatch(pod *corev1.Pod, prevStatus *SidecarInjectionStatus, revision string, annotations map[string]string,
	sic *SidecarInjectionSpec, workloadName string, mesh *meshconfig.MeshConfig) ([]byte, error) {

	var patch []rfc6902PatchOperation

	// Remove any containers previously injected by kube-inject using
	// container and volume name as unique key for removal.
	patch = append(patch, removeContainers(pod.Spec.InitContainers, prevStatus.InitContainers, "/spec/initContainers")...)
	patch = append(patch, removeContainers(pod.Spec.Containers, prevStatus.Containers, "/spec/containers")...)
	patch = append(patch, removeVolumes(pod.Spec.Volumes, prevStatus.Volumes, "/spec/volumes")...)
	patch = append(patch, removeImagePullSecrets(pod.Spec.ImagePullSecrets, prevStatus.ImagePullSecrets, "/spec/imagePullSecrets")...)

	rewrite := ShouldRewriteAppHTTPProbers(pod.Annotations, sic)

	sidecar := FindSidecar(sic.Containers)
	// We don't have to escape json encoding here when using golang libraries.
	if rewrite && sidecar != nil {
		if prober := DumpAppProbers(&pod.Spec); prober != "" {
			sidecar.Env = append(sidecar.Env, corev1.EnvVar{Name: status.KubeAppProberEnvName, Value: prober})
		}
	}

	if enablePrometheusMerge(mesh, pod.ObjectMeta.Annotations) {
		scrape := status.PrometheusScrapeConfiguration{
			Scrape: pod.ObjectMeta.Annotations["prometheus.io/scrape"],
			Path:   pod.ObjectMeta.Annotations["prometheus.io/path"],
			Port:   pod.ObjectMeta.Annotations["prometheus.io/port"],
		}
		empty := status.PrometheusScrapeConfiguration{}
		if sidecar != nil && scrape != empty {
			by, err := json.Marshal(scrape)
			if err != nil {
				return nil, err
			}
			sidecar.Env = append(sidecar.Env, corev1.EnvVar{Name: status.PrometheusScrapingConfig.Name, Value: string(by)})
		}
		annotations["prometheus.io/port"] = strconv.Itoa(int(mesh.GetDefaultConfig().GetStatusPort()))
		annotations["prometheus.io/path"] = "/stats/prometheus"
		annotations["prometheus.io/scrape"] = "true"
	}

	patch = append(patch, addContainer(pod.Spec.InitContainers, sic.InitContainers, "/spec/initContainers")...)
	patch = append(patch, addContainer(pod.Spec.Containers, sic.Containers, "/spec/containers")...)
	patch = append(patch, addVolume(pod.Spec.Volumes, sic.Volumes, "/spec/volumes")...)
	patch = append(patch, addImagePullSecrets(pod.Spec.ImagePullSecrets, sic.ImagePullSecrets, "/spec/imagePullSecrets")...)

	if sic.DNSConfig != nil {
		patch = append(patch, addPodDNSConfig(sic.DNSConfig, "/spec/dnsConfig")...)
	}

	if pod.Spec.SecurityContext != nil {
		patch = append(patch, addSecurityContext(pod.Spec.SecurityContext, "/spec/securityContext")...)
	}

	patch = append(patch, updateAnnotation(pod.Annotations, annotations)...)

	canonicalSvc, canonicalRev := extractCanonicalServiceLabels(pod.Labels, workloadName)
	patch = append(patch, addLabels(pod.Labels, map[string]string{
		label.TLSMode:                                model.IstioMutualTLSModeLabel,
		model.IstioCanonicalServiceLabelName:         canonicalSvc,
		label.IstioRev:                               revision,
		model.IstioCanonicalServiceRevisionLabelName: canonicalRev})...)

	if rewrite {
		patch = append(patch, createProbeRewritePatch(pod.Annotations, &pod.Spec, sic, mesh.GetDefaultConfig().GetStatusPort())...)
	}

	return json.Marshal(patch)
}

func enablePrometheusMerge(mesh *meshconfig.MeshConfig, anno map[string]string) bool {
	// If annotation is present, we look there first
	if val, f := anno[annotation.PrometheusMergeMetrics.Name]; f {
		bval, err := strconv.ParseBool(val)
		if err != nil {
			// This shouldn't happen since we validate earlier in the code
			log.Warnf("invalid annotation %v=%v", annotation.PrometheusMergeMetrics.Name, bval)
		} else {
			return bval
		}
	}
	// If mesh config setting is present, use that
	if mesh.GetEnablePrometheusMerge() != nil {
		return mesh.GetEnablePrometheusMerge().Value
	}
	// Otherwise, we default to enable
	return true
}

func extractCanonicalServiceLabels(podLabels map[string]string, workloadName string) (string, string) {
	return extractCanonicalServiceLabel(podLabels, workloadName), extractCanonicalServiceRevision(podLabels)
}

func extractCanonicalServiceRevision(podLabels map[string]string) string {
	if rev, ok := podLabels[model.IstioCanonicalServiceRevisionLabelName]; ok {
		return rev
	}

	if rev, ok := podLabels["app.kubernetes.io/version"]; ok {
		return rev
	}

	if rev, ok := podLabels["version"]; ok {
		return rev
	}

	return "latest"
}

func extractCanonicalServiceLabel(podLabels map[string]string, workloadName string) string {
	if svc, ok := podLabels[model.IstioCanonicalServiceLabelName]; ok {
		return svc
	}

	if svc, ok := podLabels["app.kubernetes.io/name"]; ok {
		return svc
	}

	if svc, ok := podLabels["app"]; ok {
		return svc
	}

	return workloadName
}

// Retain deprecated hardcoded container and volumes names to aid in
// backwards compatible migration to the new SidecarInjectionStatus.
var (
	initContainerName    = "istio-init"
	sidecarContainerName = "istio-proxy"

	legacyInitContainerNames = []string{initContainerName, "enable-core-dump"}
	legacyContainerNames     = []string{sidecarContainerName}
	legacyVolumeNames        = []string{"istio-certs", "istio-envoy"}
)

func injectionStatus(pod *corev1.Pod) *SidecarInjectionStatus {
	var statusBytes []byte
	if pod.ObjectMeta.Annotations != nil {
		if value, ok := pod.ObjectMeta.Annotations[annotation.SidecarStatus.Name]; ok {
			statusBytes = []byte(value)
		}
	}

	// default case when injected pod has explicit status
	var iStatus SidecarInjectionStatus
	if err := json.Unmarshal(statusBytes, &iStatus); err == nil {
		// heuristic assumes status is valid if any of the resource
		// lists is non-empty.
		if len(iStatus.InitContainers) != 0 ||
			len(iStatus.Containers) != 0 ||
			len(iStatus.Volumes) != 0 ||
			len(iStatus.ImagePullSecrets) != 0 {
			return &iStatus
		}
	}

	// backwards compatibility case when injected pod has legacy
	// status. Infer status from the list of legacy hardcoded
	// container and volume names.
	return &SidecarInjectionStatus{
		InitContainers: legacyInitContainerNames,
		Containers:     legacyContainerNames,
		Volumes:        legacyVolumeNames,
	}
}

func toAdmissionResponse(err error) *v1beta1.AdmissionResponse {
	return &v1beta1.AdmissionResponse{Result: &metav1.Status{Message: err.Error()}}
}

func (wh *Webhook) inject(ar *v1beta1.AdmissionReview, path string) *v1beta1.AdmissionResponse {
	req := ar.Request
	var pod corev1.Pod
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		handleError(fmt.Sprintf("Could not unmarshal raw object: %v %s", err,
			string(req.Object.Raw)))
		return toAdmissionResponse(err)
	}

	// Deal with potential empty fields, e.g., when the pod is created by a deployment
	podName := potentialPodName(&pod.ObjectMeta)
	if pod.ObjectMeta.Namespace == "" {
		pod.ObjectMeta.Namespace = req.Namespace
	}

	log.Infof("AdmissionReview for Kind=%v Namespace=%v Name=%v (%v) UID=%v Rfc6902PatchOperation=%v UserInfo=%v",
		req.Kind, req.Namespace, req.Name, podName, req.UID, req.Operation, req.UserInfo)
	log.Debugf("Object: %v", string(req.Object.Raw))
	log.Debugf("OldObject: %v", string(req.OldObject.Raw))

	partialInjection := false
	if !injectRequired(ignoredNamespaces, wh.Config, &pod.Spec, &pod.ObjectMeta) {
		if wasInjectedThroughIstioctl(&pod) {
			log.Infof("Performing partial injection into pre-injected pod %s/%s (injecting Multus annotation and runAsUser id)", pod.ObjectMeta.Namespace, podName)
			partialInjection = true
		} else {
			log.Infof("Skipping %s/%s due to policy check", pod.ObjectMeta.Namespace, podName)
			totalSkippedInjections.Increment()
			return &v1beta1.AdmissionResponse{
				Allowed: true,
			}
		}
	}

	// try to capture more useful namespace/name info for deployments, etc.
	// TODO(dougreid): expand to enable lookup of OWNERs recursively a la kubernetesenv
	deployMeta := pod.ObjectMeta.DeepCopy()
	deployMeta.Namespace = req.Namespace

	typeMetadata := &metav1.TypeMeta{
		Kind:       "Pod",
		APIVersion: "v1",
	}

	if len(pod.GenerateName) > 0 {
		// if the pod name was generated (or is scheduled for generation), we can begin an investigation into the controlling reference for the pod.
		var controllerRef metav1.OwnerReference
		controllerFound := false
		for _, ref := range pod.GetOwnerReferences() {
			if *ref.Controller {
				controllerRef = ref
				controllerFound = true
				break
			}
		}
		if controllerFound {
			typeMetadata.APIVersion = controllerRef.APIVersion
			typeMetadata.Kind = controllerRef.Kind

			// heuristic for deployment detection
			deployMeta.Name = controllerRef.Name
			if typeMetadata.Kind == "ReplicaSet" && strings.HasSuffix(controllerRef.Name, pod.Labels["pod-template-hash"]) {
				name := strings.TrimSuffix(controllerRef.Name, "-"+pod.Labels["pod-template-hash"])
				deployMeta.Name = name
				typeMetadata.Kind = "Deployment"
			} else if typeMetadata.Kind == "Job" && len(controllerRef.Name) > 11 {
				// If job name suffixed with `-<ten-digit-timestamp>`, trim the suffix and set kind to cron job.
				l := len(controllerRef.Name)
				if _, err := strconv.Atoi(controllerRef.Name[l-10:]); err == nil && string(controllerRef.Name[l-11]) == "-" {
					deployMeta.Name = controllerRef.Name[:l-11]
					typeMetadata.Kind = "CronJob"
					// heuristically set cron job api version to v1beta1 as it cannot be derived from pod metadata.
					// Cronjob is not GA yet and latest version is v1beta1: https://github.com/kubernetes/enhancements/pull/978
					typeMetadata.APIVersion = "batch/v1beta1"
				}
			}
		}
	}

	if deployMeta.Name == "" {
		// if we haven't been able to extract a deployment name, then just give it the pod name
		deployMeta.Name = pod.Name
	}

	var proxyGID *int64
	proxyUID, err := getProxyUID(pod)
	if err != nil {
		log.Infof("Could not get proxyUID from annotation: %v", err)
	}
	if proxyUID == nil {
		if pod.Spec.SecurityContext != nil && pod.Spec.SecurityContext.RunAsUser != nil {
			uid := uint64(*pod.Spec.SecurityContext.RunAsUser) + 1
			proxyUID = &uid
			gid := *pod.Spec.SecurityContext.RunAsUser
			// valid GID for fsGroup defaults to first int in UID range in OCP's restricted SCC
			proxyGID = &gid
		}
		for _, c := range pod.Spec.Containers {
			if c.SecurityContext != nil && c.SecurityContext.RunAsUser != nil {
				uid := uint64(*c.SecurityContext.RunAsUser) + 1
				gid := *c.SecurityContext.RunAsUser
				if proxyUID == nil || uid > *proxyUID {
					proxyUID = &uid
				}
				if proxyGID == nil {
					proxyGID = &gid
				}
			}
		}
	}
	if proxyUID == nil {
		uid := DefaultSidecarProxyUID
		proxyUID = &uid
	}
	if proxyGID == nil {
		gid := int64(DefaultSidecarProxyUID)
		proxyGID = &gid
	}

	// due to bug https://github.com/kubernetes/kubernetes/issues/57923,
	// k8s sa jwt token volume mount file is only accessible to root user, not istio-proxy(the user that istio proxy runs as).
	// workaround by https://kubernetes.io/docs/tasks/configure-pod-container/security-context/#set-the-security-context-for-a-pod
	if wh.meshConfig.SdsUdsPath != "" {
		if pod.Spec.SecurityContext == nil {
			pod.Spec.SecurityContext = &corev1.PodSecurityContext{
				FSGroup: proxyGID,
			}
		} else {
			pod.Spec.SecurityContext.FSGroup = proxyGID
		}
	}

	spec, iStatus, err := InjectionData(wh.Config.Template, wh.valuesConfig, wh.sidecarTemplateVersion, typeMetadata, deployMeta, &pod.Spec, &pod.ObjectMeta, wh.meshConfig, path, *proxyUID) // nolint: lll
	if err != nil {
		handleError(fmt.Sprintf("Injection data: err=%v spec=%v\n", err, iStatus))
		return toAdmissionResponse(err)
	}

	var patchBytes []byte
	if partialInjection {
		patchBytes, err = createPartialPatch(&pod, wh.Config.InjectedAnnotations, *proxyUID)
	} else {
		replaceProxyRunAsUserID(spec, *proxyUID)

		annotations := map[string]string{
			annotation.SidecarStatus.Name: iStatus,
		}
		rewriteCniPodSpec(annotations, spec)

		// Add all additional injected annotations
		for k, v := range wh.Config.InjectedAnnotations {
			annotations[k] = v
		}
		patchBytes, err = createPatch(&pod, injectionStatus(&pod), wh.revision,
			annotations, spec, deployMeta.Name, wh.meshConfig)
	}

	if err != nil {
		handleError(fmt.Sprintf("AdmissionResponse: err=%v spec=%v\n", err, spec))
		return toAdmissionResponse(err)
	}

	log.Debugf("AdmissionResponse: patch=%v\n", string(patchBytes))

	reviewResponse := v1beta1.AdmissionResponse{
		Allowed: true,
		Patch:   patchBytes,
		PatchType: func() *v1beta1.PatchType {
			pt := v1beta1.PatchTypeJSONPatch
			return &pt
		}(),
	}
	totalSuccessfulInjections.Increment()
	return &reviewResponse
}

func wasInjectedThroughIstioctl(pod *corev1.Pod) bool {
	_, found := pod.Annotations[annotation.SidecarStatus.Name]
	return found
}

func replaceProxyRunAsUserID(spec *SidecarInjectionSpec, proxyUID uint64) {
	for i, c := range spec.InitContainers {
		if c.Name == initContainerName {
			for j, arg := range c.Args {
				if arg == "-u" {
					spec.InitContainers[i].Args[j+1] = strconv.FormatUint(proxyUID, 10)
					break
				}
			}
			break
		}
	}
	for i, c := range spec.Containers {
		if c.Name == sidecarContainerName {
			if c.SecurityContext == nil {
				securityContext := corev1.SecurityContext{}
				spec.Containers[i].SecurityContext = &securityContext
			}
			proxyUIDasInt64 := int64(proxyUID)
			spec.Containers[i].SecurityContext.RunAsUser = &proxyUIDasInt64
			break
		}
	}
}

func createPartialPatch(pod *corev1.Pod, annotations map[string]string, proxyUID uint64) ([]byte, error) {
	var patch []rfc6902PatchOperation
	patch = append(patch, patchProxyRunAsUserID(pod, proxyUID)...)
	patch = append(patch, updateAnnotation(pod.Annotations, annotations)...)
	return json.Marshal(patch)
}

func patchProxyRunAsUserID(pod *corev1.Pod, proxyUID uint64) (patch []rfc6902PatchOperation) {
	for i, c := range pod.Spec.InitContainers {
		if c.Name == initContainerName {
			for j, arg := range c.Args {
				if arg == "-u" {
					patch = append(patch, rfc6902PatchOperation{
						Op:    "replace",
						Path:  fmt.Sprintf("/spec/initContainers/%d/args/%d", i, j+1), // j+1 because the uid is the next argument (after -u)
						Value: strconv.FormatUint(proxyUID, 10),
					})
					break
				}
			}
			break
		}
	}

	for i, c := range pod.Spec.Containers {
		if c.Name == sidecarContainerName {
			if c.SecurityContext == nil {
				proxyUIDasInt64 := int64(proxyUID)
				securityContext := corev1.SecurityContext{
					RunAsUser: &proxyUIDasInt64,
				}
				patch = append(patch, rfc6902PatchOperation{
					Op:    "add",
					Path:  fmt.Sprintf("/spec/containers/%d/securityContext", i),
					Value: securityContext,
				})
			} else if c.SecurityContext.RunAsUser == nil {
				patch = append(patch, rfc6902PatchOperation{
					Op:    "add",
					Path:  fmt.Sprintf("/spec/containers/%d/securityContext/runAsUser", i),
					Value: proxyUID,
				})
			} else {
				patch = append(patch, rfc6902PatchOperation{
					Op:    "replace",
					Path:  fmt.Sprintf("/spec/containers/%d/securityContext/runAsUser", i),
					Value: proxyUID,
				})
			}
			break
		}
	}

	return patch
}

func getProxyUID(pod corev1.Pod) (*uint64, error) {
	if pod.Annotations != nil {
		if annotationValue, found := pod.Annotations[proxyUIDAnnotation]; found {
			proxyUID, err := strconv.ParseUint(annotationValue, 10, 64)
			if err != nil {
				return nil, err
			}
			return &proxyUID, nil
		}
	}
	return nil, nil
}

func (wh *Webhook) serveInject(w http.ResponseWriter, r *http.Request) {
	totalInjections.Increment()
	var body []byte
	if r.Body != nil {
		if data, err := kube.HTTPConfigReader(r); err == nil {
			body = data
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if len(body) == 0 {
		handleError("no body found")
		http.Error(w, "no body found", http.StatusBadRequest)
		return
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		handleError(fmt.Sprintf("contentType=%s, expect application/json", contentType))
		http.Error(w, "invalid Content-Type, want `application/json`", http.StatusUnsupportedMediaType)
		return
	}

	path := ""
	if r.URL != nil {
		path = r.URL.Path
	}

	var reviewResponse *v1beta1.AdmissionResponse
	ar := v1beta1.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		handleError(fmt.Sprintf("Could not decode body: %v", err))
		reviewResponse = toAdmissionResponse(err)
	} else {
		log.Debugf("AdmissionRequest for path=%s\n", path)
		reviewResponse = wh.inject(&ar, path)
	}

	response := v1beta1.AdmissionReview{}
	if reviewResponse != nil {
		response.Response = reviewResponse
		if ar.Request != nil {
			response.Response.UID = ar.Request.UID
		}
	}

	resp, err := json.Marshal(response)
	if err != nil {
		log.Errorf("Could not encode response: %v", err)
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
	}
	if _, err := w.Write(resp); err != nil {
		log.Errorf("Could not write response: %v", err)
		http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
	}
}

func handleError(message string) {
	log.Errorf(message)
	totalFailedInjections.Increment()
}
