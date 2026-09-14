// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cognitiveservices/armcognitiveservices/v2"
)

func TestDeploymentViewAddsLifecycleMetadata(t *testing.T) {
	account := &armcognitiveservices.Account{
		ID:       to.Ptr("/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/account"),
		Name:     to.Ptr("account"),
		Kind:     to.Ptr("OpenAI"),
		Location: to.Ptr("eastus"),
	}
	deployment := &armcognitiveservices.Deployment{
		ID:   to.Ptr(accountID(account) + "/deployments/chat"),
		Name: to.Ptr("chat"),
		Properties: &armcognitiveservices.DeploymentProperties{
			Model: &armcognitiveservices.DeploymentModel{
				Format:  to.Ptr("OpenAI"),
				Name:    to.Ptr("gpt-4o"),
				Version: to.Ptr("2024-05-13"),
			},
			VersionUpgradeOption: to.Ptr(
				armcognitiveservices.DeploymentModelVersionUpgradeOptionOnceCurrentVersionExpired,
			),
		},
	}
	retirement := "2026-10-01T00:00:00Z"
	catalog := map[string]*armcognitiveservices.AccountModel{
		modelKey("OpenAI", "gpt-4o", "2024-05-13"): {
			Format:          to.Ptr("OpenAI"),
			Name:            to.Ptr("gpt-4o"),
			Version:         to.Ptr("2024-05-13"),
			LifecycleStatus: to.Ptr(armcognitiveservices.ModelLifecycleStatusDeprecating),
			Deprecation: &armcognitiveservices.ModelDeprecationInfo{
				Inference: &retirement,
			},
		},
	}

	got := deploymentView(account, "rg", deployment, catalog)
	if got.ModelName != "gpt-4o" || got.ModelVersion != "2024-05-13" {
		t.Fatalf("unexpected model identity: %+v", got)
	}
	if got.LifecycleStatus != "Deprecating" {
		t.Fatalf("unexpected lifecycle status: %q", got.LifecycleStatus)
	}
	if got.RetirementDate == nil || *got.RetirementDate != retirement {
		t.Fatalf("unexpected retirement date: %v", got.RetirementDate)
	}
	if got.VersionUpgradeOption != "OnceCurrentVersionExpired" {
		t.Fatalf("unexpected version upgrade option: %q", got.VersionUpgradeOption)
	}
}

func accountID(account *armcognitiveservices.Account) string {
	if account.ID == nil {
		return ""
	}
	return *account.ID
}
