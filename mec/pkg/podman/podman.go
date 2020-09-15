package podman

import (
	"bytes"
	"os/exec"
	"strings"
	"sync"
)

var (
	lock sync.Mutex = sync.Mutex{}
)

func Login(token, registry string) (string, error) {
	lock.Lock()
	defer lock.Unlock()

	cmd := exec.Command("podman", "--storage-driver=vfs", "login", "--tls-verify=false", "--username=mec", "--password="+token, registry)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

func Pull(image string) (string, error) {
	lock.Lock()
	defer lock.Unlock()

	cmd := exec.Command("podman", "--storage-driver=vfs", "pull", "--tls-verify=false", image)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), err
}

func Create(image string) (string, error) {
	lock.Lock()
	defer lock.Unlock()

	cmd := exec.Command("podman", "--storage-driver=vfs", "create", image, ".")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), err
}

func Copy(from, to string) (string, error) {
	lock.Lock()
	defer lock.Unlock()

	cmd := exec.Command("podman", "--storage-driver=vfs", "cp", from, to)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), err
}

func RemoveContainer(containerID string) error {
	lock.Lock()
	defer lock.Unlock()

	cmd := exec.Command("podman", "--storage-driver=vfs", "rm", containerID)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return err
	}
	return err
}

func GetImageID(image string) (string, error) {
	lock.Lock()
	defer lock.Unlock()

	cmd := exec.Command("podman", "--storage-driver=vfs", "image", "ls", "-nq", image)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), err
}
