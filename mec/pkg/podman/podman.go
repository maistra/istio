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

package podman

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"sync"
)

var lock sync.Mutex = sync.Mutex{}

type Podman interface {
	Login(token, registry string) (string, error)
	Pull(image string) (string, error)
	Create(image string) (string, error)
	Copy(from, to string) (string, error)
	RemoveContainer(containerID string) error
	GetImageID(image string) (string, error)
}

type podman struct{}

func NewPodman() Podman {
	return &podman{}
}

func (p *podman) Login(registry, token string) (string, error) {
	return p.runCommand("login", "--tls-verify=false", "--username=mec", "--password="+token, registry)
}

func (p *podman) Pull(image string) (string, error) {
	return p.runCommand("pull", "--tls-verify=false", image)
}

func (p *podman) Create(image string) (string, error) {
	return p.runCommand("create", image, ".")
}

func (p *podman) Copy(from, to string) (string, error) {
	return p.runCommand("cp", from, to)
}

func (p *podman) RemoveContainer(containerID string) error {
	_, err := p.runCommand("rm", containerID)
	return err
}

func (p *podman) GetImageID(image string) (string, error) {
	return p.runCommand("image", "ls", "-nq", image)
}

func (p *podman) runCommand(args ...string) (string, error) {
	lock.Lock()
	defer lock.Unlock()
	cmd := exec.Command("podman", append([]string{"--storage-driver=vfs"}, args...)...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	if err != nil {
		err = errors.New(strings.TrimSpace(errOut.String()))
	}
	return strings.TrimSpace(out.String()), err
}
