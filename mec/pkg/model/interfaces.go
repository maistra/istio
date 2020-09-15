package model

type ImagePullStrategy interface {
	// PullImage will go and Pull and image from a remote registry
	PullImage(image *ImageRef) (Image, error)
	// GetImage returns an image that has been pulled previously
	GetImage(image *ImageRef) (Image, error)
}

type Image interface {
	CopyWasmModule(outputFile string) error
	GetManifest() *Manifest
	SHA256() string
}
