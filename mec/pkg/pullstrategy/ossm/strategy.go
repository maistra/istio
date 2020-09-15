package ossm

import (
	"context"
	"fmt"
	"io/ioutil"
	"path"
	"strings"

	imagev1 "github.com/openshift/api/image/v1"
	imagev1client "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	"gopkg.in/yaml.v2"
	"istio.io/pkg/log"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	"istio.io/istio/mec/pkg/model"
	"istio.io/istio/mec/pkg/podman"
)

const (
	imageStreamPrefix = "ossm-extension-"
)

type ossmPullStrategy struct {
	client    *imagev1client.ImageV1Client
	namespace string
}

func NewOSSMPullStrategy(config *rest.Config, namespace string) (model.ImagePullStrategy, error) {
	cl, err := imagev1client.NewForConfig(config)
	if err != nil {
		log.Errorf("Failed to create imagev1 client: %s", err)
		return nil, err
	}

	return &ossmPullStrategy{
		client:    cl,
		namespace: namespace,
	}, nil
}

func (p *ossmPullStrategy) createImageStreamImport(client *imagev1client.ImageV1Client, image *model.ImageRef) (*imagev1.ImageStreamImport, error) {
	isi := &imagev1.ImageStreamImport{
		ObjectMeta: metav1.ObjectMeta{
			Name: imageStreamPrefix + image.Repository,
		},
		Spec: imagev1.ImageStreamImportSpec{
			Import: true,
			Images: []imagev1.ImageImportSpec{
				{
					From: corev1.ObjectReference{
						Kind: "DockerImage",
						Name: image.String(),
					},
					To: &corev1.LocalObjectReference{
						Name: image.Repository,
					},
					ReferencePolicy: imagev1.TagReferencePolicy{
						Type: imagev1.SourceTagReferencePolicy,
					},
				},
			},
		},
	}

	createdIsi, err := client.ImageStreamImports(p.namespace).Create(context.TODO(), isi, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	return createdIsi, nil
}

type ossmImage struct {
	model.Image

	imageID  string
	sha256   string
	manifest *model.Manifest
}

func (p *ossmPullStrategy) GetImage(image *model.ImageRef) (model.Image, error) {
	// only works with imageRefs that come with a SHA256 value
	if image.SHA256 == "" {
		return nil, fmt.Errorf("GetImage() only works for pinned images")
	}
	imageID, err := podman.GetImageID(image.String())
	if err != nil {
		return nil, err
	} else if imageID == "" {
		return nil, fmt.Errorf("Failed to read imageID from podman CLI")
	}
	manifest, err := extractManifest(imageID)
	if err != nil {
		log.Errorf("Failed to extract manifest from container image: %s", err)
		return nil, err
	}
	return &ossmImage{
		manifest: manifest,
		imageID:  imageID,
		sha256:   "sha256:" + image.SHA256,
	}, nil

}

// Pull retrieves an image from a remote registry
func (p *ossmPullStrategy) PullImage(image *model.ImageRef) (model.Image, error) {
	var imageStream *imagev1.ImageStream
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		imageStream, err = p.client.ImageStreams(p.namespace).Get(context.TODO(), imageStreamPrefix+image.Repository, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			createdIsi, err := p.createImageStreamImport(p.client, image)
			if err != nil {
				log.Errorf("Failed to create ImageStreamImport: %s, attempt %d", err, attempt)
				continue
			}
			log.Infof("Created ImageStreamImport %s", createdIsi.Name)
		}
		if err != nil {
			log.Errorf("Failed to Get() ImageStream: %s, attempt %d", err, attempt)
		}
	}
	if err != nil {
		return nil, err
	}
	// TODO implement importing always when ImagePullPolicy == Always
	repo := imageStream.Status.DockerImageRepository
	sha := imageStream.Status.Tags[0].Items[0].Image
	log.Infof("Pulling container image %s", repo+"@"+sha)
	imageID, err := podman.Pull(repo + "@" + sha)
	log.Infof("Pulled container image with ID %s", imageID)
	manifest, err := extractManifest(imageID)
	if err != nil {
		log.Errorf("Failed to extract manifest from container image: %s", err)
		return nil, err
	}
	return &ossmImage{
		manifest: manifest,
		imageID:  imageID,
		sha256:   sha,
	}, nil
}

func extractManifest(imageID string) (*model.Manifest, error) {
	containerID, err := podman.Create(imageID)
	log.Infof("Created container with ID %s", containerID)
	log.Infof("Extracting manifest from container with ID %s", containerID)

	tmpDir, err := ioutil.TempDir("", containerID)
	if err != nil {
		return nil, fmt.Errorf("Failed to create temp dir: %s", err)
	}
	manifestFile := path.Join(tmpDir, "manifest.yaml")
	podman.Copy(containerID+":/manifest.yaml", manifestFile)
	manifestBytes, err := ioutil.ReadFile(manifestFile)
	if err != nil {
		return nil, fmt.Errorf("Failed to read manifest.yaml: %s", err)
	}
	manifest := &model.Manifest{}
	err = yaml.Unmarshal(manifestBytes, manifest)
	if err != nil {
		return nil, fmt.Errorf("Failed to unmarshal manifest.yaml: %s", err)
	}
	err = podman.RemoveContainer(containerID)
	if err != nil {
		return nil, fmt.Errorf("Failed to remove container: %s", err)
	}
	log.Infof("Deleted container with ID %s", containerID)
	return manifest, nil
}

func (ref *ossmImage) CopyWasmModule(outputFile string) error {
	containerID, err := podman.Create(ref.imageID)
	if err != nil {
		return err
	}
	log.Infof("Created container with ID %s", containerID)
	log.Infof("Extracting WASM module from container with ID %s", containerID)
	_, err = podman.Copy(containerID+":/"+ref.manifest.Module, outputFile)
	if err != nil {
		return err
	}
	err = podman.RemoveContainer(containerID)
	if err != nil {
		return err
	}
	log.Infof("Deleted container with ID %s", containerID)
	return nil
}

func (ref *ossmImage) GetManifest() *model.Manifest {
	return ref.manifest
}

func (ref *ossmImage) SHA256() string {
	return strings.Split(ref.sha256, "sha256:")[1]
}
