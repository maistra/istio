// Copyright 2017 Istio Authors
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

package crd

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strings"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schemas"
	"istio.io/istio/pkg/util/gogoprotomarshal"
)

// ConvertObject converts an IstioObject k8s-style object to the
// internal configuration model.
func ConvertObject(schema schema.Instance, object IstioObject, domain string) (*model.Config, error) {
	data, err := schema.FromJSONMap(object.GetSpec())
	if err != nil {
		return nil, err
	}
	meta := object.GetObjectMeta()

	return &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:              schema.Type,
			Group:             ResourceGroup(&schema),
			Version:           schema.Version,
			Name:              meta.Name,
			Namespace:         meta.Namespace,
			Domain:            domain,
			Labels:            meta.Labels,
			Annotations:       meta.Annotations,
			ResourceVersion:   meta.ResourceVersion,
			CreationTimestamp: meta.CreationTimestamp.Time,
		},
		Spec: data,
	}, nil
}

// ConvertObjectFromUnstructured converts an IstioObject k8s-style object to the
// internal configuration model.
func ConvertObjectFromUnstructured(schema schema.Instance, un *unstructured.Unstructured, domain string) (*model.Config, error) {
	data, err := schema.FromJSONMap(un.Object["spec"])
	if err != nil {
		return nil, err
	}

	return &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:              schema.Type,
			Group:             ResourceGroup(&schema),
			Version:           schema.Version,
			Name:              un.GetName(),
			Namespace:         un.GetNamespace(),
			Domain:            domain,
			Labels:            un.GetLabels(),
			Annotations:       un.GetAnnotations(),
			ResourceVersion:   un.GetResourceVersion(),
			CreationTimestamp: un.GetCreationTimestamp().Time,
		},
		Spec: data,
	}, nil
}

// ConvertConfig translates Istio config to k8s config JSON
func ConvertConfig(schema schema.Instance, cfg model.Config) (IstioObject, error) {
	spec, err := gogoprotomarshal.ToJSONMap(cfg.Spec)
	if err != nil {
		return nil, err
	}
	namespace := cfg.Namespace
	if namespace == "" {
		namespace = meta_v1.NamespaceDefault
	}
	out := KnownTypes[schema.Type].Object.DeepCopyObject().(IstioObject)
	out.SetObjectMeta(meta_v1.ObjectMeta{
		Name:            cfg.Name,
		Namespace:       namespace,
		ResourceVersion: cfg.ResourceVersion,
		Labels:          cfg.Labels,
		Annotations:     cfg.Annotations,
	})
	out.SetSpec(spec)

	return out, nil
}

// ResourceName converts "my-name" to "myname".
// This is needed by k8s API server as dashes prevent kubectl from accessing CRDs
func ResourceName(s string) string {
	return strings.Replace(s, "-", "", -1)
}

// ResourceGroup generates the k8s API group for each schema.
func ResourceGroup(schema *schema.Instance) string {
	if schema.GroupDomain != "" {
		return schema.Group + "." + schema.GroupDomain
	}
	return schema.Group + "." + constants.IstioAPIGroupDomain
}

// TODO - add special cases for type-to-kind and kind-to-type
// conversions with initial-isms. Consider adding additional type
// information to the abstract model and/or elevating k8s
// representation to first-class type to avoid extra conversions.

// KebabCaseToCamelCase converts "my-name" to "MyName"
func KebabCaseToCamelCase(s string) string {
	switch s {
	case "http-api-spec":
		return "HTTPAPISpec"
	case "http-api-spec-binding":
		return "HTTPAPISpecBinding"
	default:
		words := strings.Split(s, "-")
		out := ""
		for _, word := range words {
			out += strings.Title(word)
		}
		return out
	}
}

// CamelCaseToKebabCase converts "MyName" to "my-name"
func CamelCaseToKebabCase(s string) string {
	switch s {
	case "HTTPAPISpec":
		return "http-api-spec"
	case "HTTPAPISpecBinding":
		return "http-api-spec-binding"
	default:
		var out bytes.Buffer
		for i := range s {
			if 'A' <= s[i] && s[i] <= 'Z' {
				if i > 0 {
					out.WriteByte('-')
				}
				out.WriteByte(s[i] - 'A' + 'a')
			} else {
				out.WriteByte(s[i])
			}
		}
		return out.String()
	}
}

func parseInputsImpl(inputs string, withValidate bool) ([]model.Config, []IstioKind, error) {
	var varr []model.Config
	var others []IstioKind
	reader := bytes.NewReader([]byte(inputs))
	var empty = IstioKind{}

	// We store configs as a YaML stream; there may be more than one decoder.
	yamlDecoder := kubeyaml.NewYAMLOrJSONDecoder(reader, 512*1024)
	for {
		obj := IstioKind{}
		err := yamlDecoder.Decode(&obj)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("cannot parse proto message: %v", err)
		}
		if reflect.DeepEqual(obj, empty) {
			continue
		}

		s, exists := schemas.Istio.GetByType(CamelCaseToKebabCase(obj.Kind))
		if !exists {
			log.Debugf("unrecognized type %v", obj.Kind)
			others = append(others, obj)
			continue
		}

		cfg, err := ConvertObject(s, &obj, "")
		if err != nil {
			return nil, nil, fmt.Errorf("cannot parse proto message: %v", err)
		}

		if withValidate {
			if err := s.Validate(cfg.Name, cfg.Namespace, cfg.Spec); err != nil {
				return nil, nil, fmt.Errorf("configuration is invalid: %v", err)
			}
		}

		varr = append(varr, *cfg)
	}

	return varr, others, nil
}

// ParseInputs reads multiple documents from `kubectl` output and checks with
// the schema. It also returns the list of unrecognized kinds as the second
// response.
//
// NOTE: This function only decodes a subset of the complete k8s
// ObjectMeta as identified by the fields in model.ConfigMeta. This
// would typically only be a problem if a user dumps an configuration
// object with kubectl and then re-ingests it.
func ParseInputs(inputs string) ([]model.Config, []IstioKind, error) {
	return parseInputsImpl(inputs, true)
}

// ParseInputsWithoutValidation same as ParseInputs, but do not apply schema validation.
func ParseInputsWithoutValidation(inputs string) ([]model.Config, []IstioKind, error) {
	return parseInputsImpl(inputs, false)
}
