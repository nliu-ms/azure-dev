// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"github.com/azure/azure-dev/cli/azd/pkg/azdext"
	"github.com/spf13/cobra"
)

func newMigrateCommand() *cobra.Command {
	flags := &migrateWebFlags{}
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Review deployed model versions and retirement timelines",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := azdext.WithAccessToken(cmd.Context())
			return runMigrateWeb(ctx, cmd.OutOrStdout(), cmd.ErrOrStderr(), flags)
		},
	}
	cmd.Flags().StringVarP(&flags.SubscriptionID, "subscription", "s", "", "Azure subscription ID")
	cmd.Flags().IntVar(&flags.Port, "port", 0, "Local Web UI port (uses an available port when omitted)")
	return cmd
}
