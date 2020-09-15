package model

import (
	"fmt"
	"strings"

	"istio.io/istio/pkg/servicemesh/apis/servicemesh/v1alpha1"
)

type ImageRef struct {
	Hub        string
	Repository string
	Tag        string
	SHA256     string
}

type Manifest struct {
	SchemaVersion ManifestSchemaVersion `json:"schemaVersion"`
	Name          string                `json:"name"`
	Description   string                `json:"description"`
	Version       string                `json:"version"`
	Phase         v1alpha1.FilterPhase  `json:"phase"`
	Priority      int                   `json:"priority"`
	Module        string                `json:"module"`
}

type ManifestSchemaVersion string

const (
	ManifestSchemaVersion1 = "1"
)

func (i *ImageRef) String() string {
	tag := i.Tag
	if i.SHA256 != "" {
		tag = fmt.Sprintf("sha256:%s", i.SHA256)
	}
	return fmt.Sprintf("%s/%s:%s", i.Hub, i.Repository, tag)
}

func StringToImageRef(ref string) *ImageRef {
	colonSplit := strings.SplitN(ref, ":", 2)
	tag := colonSplit[1]
	slashSplit := strings.Split(colonSplit[0], "/")
	repo := slashSplit[len(slashSplit)-1]
	hub := strings.Join(slashSplit[:len(slashSplit)-1], "/")

	shaIndex := strings.Index(tag, "sha256:")
	if shaIndex == -1 {
		return &ImageRef{
			Hub:        hub,
			Tag:        tag,
			Repository: repo,
		}
	}

	return &ImageRef{
		Hub:        hub,
		SHA256:     tag[shaIndex+7:],
		Repository: repo,
	}
}
