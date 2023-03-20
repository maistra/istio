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

package install

import (
	"os"
	"path/filepath"

	"istio.io/istio/pkg/file"
	"istio.io/istio/pkg/util/sets"
)

func copyBinaries(srcDir string, targetDirs []string, updateBinaries bool, skipBinaries []string) error {
	skipBinariesSet := sets.New(skipBinaries...)
	for _, targetDir := range targetDirs {
		if err := file.IsDirWriteable(targetDir); err != nil {
			installLog.Infof("Directory %s is not writable, skipping.", targetDir)
			continue
		}

		files, err := os.ReadDir(srcDir)
		if err != nil {
			return err
		}

		for _, f := range files {
			if f.IsDir() {
				continue
			}

			filename := f.Name()
			if skipBinariesSet.Contains(filename) {
				installLog.Infof("%s is in SKIP_CNI_BINARIES, skipping.", filename)
				continue
			}

			targetFilepath := filepath.Join(targetDir, filename)
			if _, err := os.Stat(targetFilepath); err == nil && !updateBinaries {
				installLog.Infof("%s is already here and UPDATE_CNI_BINARIES isn't true, skipping", targetFilepath)
				continue
			}

			srcFilepath := filepath.Join(srcDir, filename)
			err := file.AtomicCopy(srcFilepath, targetDir, filename)
			if err != nil {
				return err
			}
			installLog.Infof("Copied %s to %s.", filename, targetDir)
		}
	}

	return nil
}
