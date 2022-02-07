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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/constants"
	"istio.io/istio/cni/pkg/util"
	"istio.io/istio/pkg/file"
	"istio.io/pkg/log"
)

var installLog = log.RegisterScope("install", "CNI install", 0)

type Installer struct {
	cfg                *config.InstallConfig
	isReady            *atomic.Value
	saToken            string
	kubeconfigFilepath string
	cniConfigFilepath  string
}

// NewInstaller returns an instance of Installer with the given config
func NewInstaller(cfg *config.InstallConfig, isReady *atomic.Value) *Installer {
	return &Installer{
		cfg:     cfg,
		isReady: isReady,
	}
}

func (in *Installer) install(ctx context.Context) (err error) {
	if err = copyBinaries(
		in.cfg.CNIBinSourceDir, in.cfg.CNIBinTargetDirs,
		in.cfg.UpdateCNIBinaries, in.cfg.SkipCNIBinaries, in.cfg.CNIBinariesPrefix); err != nil {
		cniInstalls.With(resultLabel.Value(resultCopyBinariesFailure)).Increment()
		return
	}

	if in.saToken, err = readServiceAccountToken(); err != nil {
		cniInstalls.With(resultLabel.Value(resultReadSAFailure)).Increment()
		return
	}

	if in.kubeconfigFilepath, err = createKubeconfigFile(in.cfg, in.saToken); err != nil {
		cniInstalls.With(resultLabel.Value(resultCreateKubeConfigFailure)).Increment()
		return
	}

	if in.cniConfigFilepath, err = createCNIConfigFile(ctx, in.cfg, in.saToken); err != nil {
		cniInstalls.With(resultLabel.Value(resultCreateCNIConfigFailure)).Increment()
		return
	}

	return
}

// Run starts the installation process, verifies the configuration, then sleeps.
// If an invalid configuration is detected, the installation process will restart to restore a valid state.
func (in *Installer) Run(ctx context.Context) (err error) {
	if err = in.install(ctx); err != nil {
		return
	}

	installLog.Info("Installation succeed, start watching for re-installation.")
	for {
		if err = sleepCheckInstall(ctx, in.cfg, in.cniConfigFilepath, in.isReady); err != nil {
			return
		}

		installLog.Info("Detect changes to the CNI configuration and binaries, attempt reinstalling...")
		if in.cfg.CNIEnableReinstall {
			if err = in.install(ctx); err != nil {
				return
			}
			installLog.Info("CNI configuration and binaries reinstalled.")
		} else {
			installLog.Info("Skip reinstalling CNI configuration and binaries.")
		}
	}
}

// Cleanup remove Istio CNI's config, kubeconfig file, and binaries.
func (in *Installer) Cleanup() error {
	istioCniExecutableName := in.cfg.CNIBinariesPrefix + "istio-cni"

	installLog.Info("Cleaning up.")
	if len(in.cniConfigFilepath) > 0 && file.Exists(in.cniConfigFilepath) {
		if in.cfg.ChainedCNIPlugin {
			installLog.Infof("Removing Istio CNI config from CNI config file: %s", in.cniConfigFilepath)

			// Read JSON from CNI config file
			cniConfigMap, err := util.ReadCNIConfigMap(in.cniConfigFilepath)
			if err != nil {
				return err
			}
			// Find Istio CNI and remove from plugin list
			plugins, err := util.GetPlugins(cniConfigMap)
			if err != nil {
				return fmt.Errorf("%s: %w", in.cniConfigFilepath, err)
			}
			for i, rawPlugin := range plugins {
				plugin, err := util.GetPlugin(rawPlugin)
				if err != nil {
					return fmt.Errorf("%s: %w", in.cniConfigFilepath, err)
				}
				if plugin["type"] == istioCniExecutableName {
					cniConfigMap["plugins"] = append(plugins[:i], plugins[i+1:]...)
					break
				}
			}

			cniConfig, err := util.MarshalCNIConfig(cniConfigMap)
			if err != nil {
				return err
			}
			if err = file.AtomicWrite(in.cniConfigFilepath, cniConfig, os.FileMode(0o644)); err != nil {
				return err
			}
		} else {
			installLog.Infof("Removing Istio CNI config file: %s", in.cniConfigFilepath)
			if err := os.Remove(in.cniConfigFilepath); err != nil {
				return err
			}
		}
	}

	if len(in.kubeconfigFilepath) > 0 && file.Exists(in.kubeconfigFilepath) {
		installLog.Infof("Removing Istio CNI kubeconfig file: %s", in.kubeconfigFilepath)
		if err := os.Remove(in.kubeconfigFilepath); err != nil {
			return err
		}
	}

	for _, targetDir := range in.cfg.CNIBinTargetDirs {
		if istioCNIBin := filepath.Join(targetDir, istioCniExecutableName); file.Exists(istioCNIBin) {
			installLog.Infof("Removing binary: %s", istioCNIBin)
			if err := os.Remove(istioCNIBin); err != nil {
				return err
			}
		}
	}
	return nil
}

func readServiceAccountToken() (string, error) {
	saToken := constants.ServiceAccountPath + "/token"
	if !file.Exists(saToken) {
		return "", fmt.Errorf("service account token file %s does not exist. Is this not running within a pod?", saToken)
	}

	token, err := os.ReadFile(saToken)
	if err != nil {
		return "", err
	}

	return string(token), nil
}

// sleepCheckInstall verifies the configuration then blocks until an invalid configuration is detected, and return nil.
// If an error occurs or context is canceled, the function will return the error.
// Returning from this function will set the pod to "NotReady".
func sleepCheckInstall(ctx context.Context, cfg *config.InstallConfig, cniConfigFilepath string, isReady *atomic.Value) error {
	// Create file watcher before checking for installation
	// so that no file modifications are missed while and after checking
	watcher, fileModified, errChan, err := util.CreateFileWatcher(append(cfg.CNIBinTargetDirs, cfg.MountedCNINetDir)...)
	if err != nil {
		return err
	}
	defer func() {
		SetNotReady(isReady)
		_ = watcher.Close()
	}()

	for {
		if checkErr := checkInstall(cfg, cniConfigFilepath); checkErr != nil {
			// Pod set to "NotReady" due to invalid configuration
			installLog.Infof("Invalid configuration. %v", checkErr)
			return nil
		}
		// Check if file has been modified or if an error has occurred during checkInstall before setting isReady to true
		select {
		case <-fileModified:
			return nil
		case err := <-errChan:
			return err
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Valid configuration; set isReady to true and wait for modifications before checking again
			SetReady(isReady)
			cniInstalls.With(resultLabel.Value(resultSuccess)).Increment()
			// Pod set to "NotReady" before termination
			return util.WaitForFileMod(ctx, fileModified, errChan)
		}
	}
}

// checkInstall returns an error if an invalid CNI configuration is detected
func checkInstall(cfg *config.InstallConfig, cniConfigFilepath string) error {
	istioCniExecutableName := cfg.CNIBinariesPrefix + "istio-cni"

	defaultCNIConfigFilename, err := getDefaultCNINetwork(cfg.MountedCNINetDir)
	if err != nil {
		return err
	}
	defaultCNIConfigFilepath := filepath.Join(cfg.MountedCNINetDir, defaultCNIConfigFilename)
	if defaultCNIConfigFilepath != cniConfigFilepath {
		if len(cfg.CNIConfName) > 0 {
			// Install was run with overridden CNI config file so don't error out on preempt check
			// Likely the only use for this is testing the script
			installLog.Warnf("CNI config file %s preempted by %s", cniConfigFilepath, defaultCNIConfigFilepath)
		} else {
			return fmt.Errorf("CNI config file %s preempted by %s", cniConfigFilepath, defaultCNIConfigFilepath)
		}
	}

	if !file.Exists(cniConfigFilepath) {
		return fmt.Errorf("CNI config file removed: %s", cniConfigFilepath)
	}

	if cfg.ChainedCNIPlugin {
		// Verify that Istio CNI config exists in the CNI config plugin list
		cniConfigMap, err := util.ReadCNIConfigMap(cniConfigFilepath)
		if err != nil {
			return err
		}
		plugins, err := util.GetPlugins(cniConfigMap)
		if err != nil {
			return fmt.Errorf("%s: %w", cniConfigFilepath, err)
		}
		for _, rawPlugin := range plugins {
			plugin, err := util.GetPlugin(rawPlugin)
			if err != nil {
				return fmt.Errorf("%s: %w", cniConfigFilepath, err)
			}
			if plugin["type"] == istioCniExecutableName {
				return nil
			}
		}

		return fmt.Errorf("istio-cni CNI config removed from CNI config file: %s", cniConfigFilepath)
	}
	// Verify that Istio CNI config exists as a standalone plugin
	cniConfigMap, err := util.ReadCNIConfigMap(cniConfigFilepath)
	if err != nil {
		return err
	}

	if cniConfigMap["type"] != istioCniExecutableName {
		return fmt.Errorf("istio-cni CNI config file modified: %s", cniConfigFilepath)
	}
	return nil
}
