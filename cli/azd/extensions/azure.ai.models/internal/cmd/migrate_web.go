// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"azure.ai.models/internal/migrationweb"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/azure/azure-dev/cli/azd/pkg/azdext"
	"github.com/cli/browser"
)

type migrateWebFlags struct {
	SubscriptionID string
	Port           int
}

func runMigrateWeb(
	ctx context.Context,
	writer io.Writer,
	errWriter io.Writer,
	flags *migrateWebFlags,
) error {
	subscriptionID, tenantID, err := resolveMigrationSubscription(ctx, flags.SubscriptionID)
	if err != nil {
		return err
	}

	credential, err := azidentity.NewAzureDeveloperCLICredential(
		&azidentity.AzureDeveloperCLICredentialOptions{
			TenantID:                   tenantID,
			AdditionallyAllowedTenants: []string{"*"},
		},
	)
	if err != nil {
		return fmt.Errorf("create Azure credential: %w", err)
	}

	provider, err := migrationweb.NewAzureModelProvider(subscriptionID, credential)
	if err != nil {
		return err
	}
	promptOptimizer, err := migrationweb.NewPromptV2Client(credential)
	if err != nil {
		return err
	}

	server, err := migrationweb.NewServer(migrationweb.ServerOptions{
		Port:            flags.Port,
		SubscriptionID:  subscriptionID,
		Provider:        provider,
		PromptOptimizer: promptOptimizer,
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(writer, "Model Migration: %s\n", server.URL())
	fmt.Fprintln(writer, "Press Ctrl+C to stop the local server.")

	if err := browser.OpenURL(server.BrowserURL()); err != nil {
		fmt.Fprintf(
			errWriter,
			"Could not open the browser: %v\nCheck the default browser configuration and run the command again.\n",
			err,
		)
	}

	return server.Serve(ctx)
}

func resolveMigrationSubscription(ctx context.Context, explicit string) (string, string, error) {
	azdClient, err := azdext.NewAzdClient()
	if err != nil {
		return "", "", fmt.Errorf("connect to azd: %w", err)
	}
	defer azdClient.Close()

	subscriptionID := explicit
	if subscriptionID == "" {
		_, subscriptionID = loadFromEnvironment(ctx, azdClient)
	}
	if subscriptionID == "" {
		subscriptionID = os.Getenv("AZURE_SUBSCRIPTION_ID")
	}

	if subscriptionID == "" {
		if rootFlags.NoPrompt {
			return "", "", errors.New(
				"Azure subscription is required; pass --subscription or configure AZURE_SUBSCRIPTION_ID",
			)
		}
		resp, err := azdClient.Prompt().PromptSubscription(ctx, &azdext.PromptSubscriptionRequest{})
		if err != nil {
			return "", "", fmt.Errorf("select Azure subscription: %w", err)
		}
		return resp.Subscription.Id, resp.Subscription.UserTenantId, nil
	}

	tenant, err := azdClient.Account().LookupTenant(ctx, &azdext.LookupTenantRequest{
		SubscriptionId: subscriptionID,
	})
	if err != nil {
		return "", "", fmt.Errorf("resolve tenant for subscription %q: %w", subscriptionID, err)
	}
	return subscriptionID, tenant.TenantId, nil
}
