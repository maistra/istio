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

package dependencies

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/spf13/viper"

	"istio.io/istio/pkg/backoff"
	"istio.io/istio/pkg/log"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

func (r *RealDependencies) execute(cmd string, ignoreErrors bool, stdin io.Reader, args ...string) error {
	if r.CNIMode && r.HostNSEnterExec {
		originalCmd := cmd
		cmd = constants.NSENTER
		args = append([]string{fmt.Sprintf("--net=%v", r.NetworkNamespace), "--", originalCmd}, args...)
	}
	log.Infof("Running command: %s %s", cmd, strings.Join(args, " "))

	externalCommand := exec.Command(cmd, args...)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	externalCommand.Stdout = stdout
	externalCommand.Stderr = stderr
	externalCommand.Stdin = stdin

	// Grab all viper config and propagate it as environment variables to the child process
	repl := strings.NewReplacer("-", "_")
	for _, k := range viper.AllKeys() {
		v := viper.Get(k)
		if v == nil {
			continue
		}
		externalCommand.Env = append(externalCommand.Env, fmt.Sprintf("%s=%v", strings.ToUpper(repl.Replace(k)), v))
	}
	var err error
	var nsContainer ns.NetNS
	if r.CNIMode && !r.HostNSEnterExec {
		nsContainer, err = ns.GetNS(r.NetworkNamespace)
		if err != nil {
			return err
		}

		err = nsContainer.Do(func(ns.NetNS) error {
			return externalCommand.Run()
		})
		nsContainer.Close()
	} else {
		err = externalCommand.Run()
	}

	if len(stdout.String()) != 0 {
		log.Infof("Command output: \n%v", stdout.String())
	}

	if !ignoreErrors && len(stderr.Bytes()) != 0 {
		log.Errorf("Command error output: \n%v", stderr.String())
	}

	return err
}

func (r *RealDependencies) executeXTables(cmd string, ignoreErrors bool, stdin io.ReadSeeker, args ...string) error {
	if r.CNIMode && r.HostNSEnterExec {
		originalCmd := cmd
		cmd = constants.NSENTER
		args = append([]string{fmt.Sprintf("--net=%v", r.NetworkNamespace), "--", originalCmd}, args...)
	}
	log.Infof("Running command: %s %s", cmd, strings.Join(args, " "))

	var stdout, stderr *bytes.Buffer
	var err error
	var nsContainer ns.NetNS

	if r.CNIMode && !r.HostNSEnterExec {
		nsContainer, err = ns.GetNS(r.NetworkNamespace)
		if err != nil {
			return err
		}
		defer nsContainer.Close()
	}
	o := backoff.Option{
		InitialInterval: 100 * time.Millisecond,
		MaxInterval:     2 * time.Second,
	}
	b := backoff.NewExponentialBackOff(o)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	backoffError := b.RetryWithContext(ctx, func() error {
		externalCommand := exec.Command(cmd, args...)
		stdout = &bytes.Buffer{}
		stderr = &bytes.Buffer{}
		externalCommand.Stdout = stdout
		externalCommand.Stderr = stderr
		externalCommand.Stdin = stdin
		if stdin != nil {
			if _, err := stdin.Seek(0, io.SeekStart); err != nil {
				return err
			}
		}
		if r.CNIMode && !r.HostNSEnterExec {
			err = nsContainer.Do(func(ns.NetNS) error {
				return externalCommand.Run()
			})
		} else {
			err = externalCommand.Run()
		}
		exitCode, ok := exitCode(err)
		if !ok {
			// cannot get exit code. consider this as non-retriable.
			return nil
		}

		if !isXTablesLockError(stderr, exitCode) {
			// Command succeeded, or failed not because of xtables lock.
			return nil
		}

		// If command failed because xtables was locked, try the command again.
		// Note we retry invoking iptables command explicitly instead of using the `-w` option of iptables,
		// because as of iptables 1.6.x (version shipped with bionic), iptables-restore does not support `-w`.
		log.Debugf("Failed to acquire XTables lock, retry iptables command..")
		return err
	})
	if backoffError != nil {
		return fmt.Errorf("timed out trying to acquire XTables lock: %v", err)
	}

	if len(stdout.String()) != 0 {
		log.Infof("Command output: \n%v", stdout.String())
	}

	// TODO Check naming and redirection logic
	if (err != nil || len(stderr.String()) != 0) && !ignoreErrors {
		stderrStr := stderr.String()

		// Transform to xtables-specific error messages with more useful and actionable hints.
		if err != nil {
			stderrStr = transformToXTablesErrorMessage(stderrStr, err)
		}

		log.Errorf("Command error output: %v", stderrStr)
	}

	return err
}
