// Copyright 2020 Istio Authors
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

package probe

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	probeLongDesc = `Check the liveness or readiness of a locally-running server

Check if the target file is older than the interval. The locally-running server updates the last modified time
(you need to enable it on server side), so probe command compares it with current time.`

	probeExample = `
  # Check if the target file '/health' is older than interval of 4 seconds
  probe --probe-path=/health --interval=4s`
)

// CobraCommand returns a command used to probe liveness or readiness of a locally-running server
// by checking the last modified time of target file.
func CobraCommand() *cobra.Command {
	var probeOptions Options

	prb := &cobra.Command{
		Use:     "probe",
		Short:   "Check the liveness or readiness of a locally-running server",
		Long:    probeLongDesc,
		Args:    cobra.ExactArgs(0),
		Example: probeExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := probeOptions.Validate(); err != nil {
				return err
			}
			if err := NewFileClient(&probeOptions).GetStatus(); err != nil {
				return fmt.Errorf("fail on inspecting path %s: %v", probeOptions.Path, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "OK")
			return nil
		},
	}
	prb.PersistentFlags().StringVar(&probeOptions.Path, "probe-path", "",
		"Path of the file for checking the last modified time.")
	prb.PersistentFlags().DurationVar(&probeOptions.UpdateInterval, "interval", 0,
		"Duration used for checking the target file's last modified time.")

	return prb
}
