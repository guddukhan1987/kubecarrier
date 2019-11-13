/*
Copyright 2019 The Kubecarrier Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmd

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/kubermatic/kubecarrier/pkg/anchor/internal/cmd/completion"
	e2e_test "github.com/kubermatic/kubecarrier/pkg/anchor/internal/cmd/e2e-test"
	"github.com/kubermatic/kubecarrier/pkg/anchor/internal/cmd/setup"
	"github.com/kubermatic/kubecarrier/pkg/anchor/internal/cmd/version"
)

type levelAccessor interface {
	GetLevel() *zap.AtomicLevel
}

type flagpole struct {
	Verbosity int8
}

// NewAnchor creates the root command for the anchor CLI.
func NewAnchor(log logr.Logger) *cobra.Command {
	flags := &flagpole{}
	cmd := &cobra.Command{
		Use:   "anchor",
		Short: "Anchor is the CLI tool for managing Kubecarrier",
		Long: `Anchor is a CLI library for managing Kubecarrier,
Documentation is available in the project's repository:
https://github.com/kubermatic/kubecarrier`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			fmt.Println("this is executed")
			la, ok := log.(levelAccessor)
			if !ok {
				fmt.Println("not level Accessor")
				return
			}
			fmt.Println("level Accessor")
			la.GetLevel().SetLevel(zapcore.Level(-1 * flags.Verbosity))
		},
	}

	cmd.PersistentFlags().Int8VarP(&flags.Verbosity, "verbosity", "v", 0, "logging verbosity")

	cmd.AddCommand(
		completion.NewCommand(log),
		e2e_test.NewCommand(log),
		setup.NewCommand(log),
		version.NewCommand(log),
	)

	return cmd
}
