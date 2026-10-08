// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cognitiveservices/armcognitiveservices/v2"
)

func TestDeploymentViewAddsLifecycleMetadata(t *testing.T) {
	account := &armcognitiveservices.Account{
		ID:       new("/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/account"),
		Name:     new("account"),
		Kind:     new("OpenAI"),
		Location: new("eastus"),
	}
	deployment := &armcognitiveservices.Deployment{
		ID:   new(accountID(account) + "/deployments/chat"),
		Name: new("chat"),
		Properties: &armcognitiveservices.DeploymentProperties{
			Model: &armcognitiveservices.DeploymentModel{
				Format:  new("OpenAI"),
				Name:    new("gpt-4o"),
				Version: new("2024-05-13"),
			},
			VersionUpgradeOption: to.Ptr(
				armcognitiveservices.DeploymentModelVersionUpgradeOptionOnceCurrentVersionExpired,
			),
		},
	}
	retirement := "2026-10-01T00:00:00Z"
	catalog := map[string]*armcognitiveservices.AccountModel{
		modelKey("OpenAI", "gpt-4o", "2024-05-13"): {
			Format:          new("OpenAI"),
			Name:            new("gpt-4o"),
			Version:         new("2024-05-13"),
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

func TestTargetAssessmentUsesDefaultVersionAndCapabilities(t *testing.T) {
	retirement := "2027-09-02T00:00:00Z"
	models := map[string]*armcognitiveservices.AccountModel{
		modelKey("OpenAI", "gpt-5.4", "2026-01-01"): {
			Format:  new("OpenAI"),
			Name:    new("gpt-5.4"),
			Version: new("2026-01-01"),
		},
		modelKey("OpenAI", "gpt-5.4", "2026-03-05"): {
			Format:           new("OpenAI"),
			Name:             new("gpt-5.4"),
			Version:          new("2026-03-05"),
			IsDefaultVersion: new(true),
			LifecycleStatus:  to.Ptr(armcognitiveservices.ModelLifecycleStatusGenerallyAvailable),
			Deprecation: &armcognitiveservices.ModelDeprecationInfo{
				Inference: &retirement,
			},
			Capabilities: map[string]*string{
				"responses": new("true"),
			},
			SKUs: []*armcognitiveservices.ModelSKU{{
				Name: new("GlobalStandard"),
				Capacity: &armcognitiveservices.CapacityConfig{
					Default: new(int32(10)),
					Maximum: new(int32(1_000_000)),
				},
			}},
		},
	}

	selected := selectTargetModel(models, "OpenAI", "gpt-5.4", "")
	if selected == nil || value(selected.Version) != "2026-03-05" {
		t.Fatalf("unexpected selected target: %+v", selected)
	}

	result := targetAssessment(
		TargetAssessment{
			TargetModel:  "gpt-5.4",
			TargetFormat: "OpenAI",
			Capabilities: []ModelCapability{},
		},
		selected,
	)
	if result.LifecycleStatus != "GenerallyAvailable" {
		t.Fatalf("unexpected target lifecycle: %+v", result)
	}
	if len(result.Capabilities) != 1 || result.Capabilities[0].Name != "responses" {
		t.Fatalf("unexpected capabilities: %+v", result.Capabilities)
	}
}

func TestSelectTargetModelReturnsNilWhenTargetIsUnavailable(t *testing.T) {
	models := map[string]*armcognitiveservices.AccountModel{
		modelKey("OpenAI", "gpt-4o", "2024-05-13"): {
			Format:  new("OpenAI"),
			Name:    new("gpt-4o"),
			Version: new("2024-05-13"),
		},
	}

	if selected := selectTargetModel(models, "OpenAI", "gpt-5.4", ""); selected != nil {
		t.Fatalf("expected target to be unavailable, got %+v", selected)
	}
}

func TestSelectTargetModelUsesRequestedExistingDeploymentVersion(t *testing.T) {
	models := map[string]*armcognitiveservices.AccountModel{
		modelKey("OpenAI", "gpt-5.4", "2026-01-01"): {
			Format:           new("OpenAI"),
			Name:             new("gpt-5.4"),
			Version:          new("2026-01-01"),
			IsDefaultVersion: new(false),
		},
		modelKey("OpenAI", "gpt-5.4", "2026-03-05"): {
			Format:           new("OpenAI"),
			Name:             new("gpt-5.4"),
			Version:          new("2026-03-05"),
			IsDefaultVersion: new(true),
		},
	}

	selected := selectTargetModel(models, "OpenAI", "gpt-5.4", "2026-01-01")
	if selected == nil || value(selected.Version) != "2026-01-01" {
		t.Fatalf("unexpected selected target version: %+v", selected)
	}
}

func TestDeploymentOptionsMatchCapacityAndQuotaBySKU(t *testing.T) {
	target := &armcognitiveservices.AccountModel{
		SKUs: []*armcognitiveservices.ModelSKU{
			{
				Name:      new("Standard"),
				UsageName: new("OpenAI.Standard.gpt-5.4"),
			},
			{
				Name:      new("GlobalStandard"),
				UsageName: new("OpenAI.GlobalStandard.gpt-5.4"),
			},
			{
				Name:      new("DataZoneStandard"),
				UsageName: new("OpenAI.DataZoneStandard.gpt-5.4"),
			},
			{
				Name:      new("GlobalBatch"),
				UsageName: new("OpenAI.GlobalBatch.gpt-5.4"),
			},
		},
	}
	result := deploymentOptionsForModel(
		DeploymentOptions{
			TargetModel: "gpt-5.4",
			Region:      "eastus",
			SKUs:        []DeploymentSKUOption{},
		},
		target,
		map[string]*float32{
			"globalstandard": new(float32(120)),
		},
		map[string]*armcognitiveservices.Usage{
			"openai.globalstandard.gpt-5.4": {
				CurrentValue: new(float64(20)),
				Limit:        new(float64(100)),
				Name: &armcognitiveservices.MetricName{
					Value: new("OpenAI.GlobalStandard.gpt-5.4"),
				},
			},
		},
	)

	if len(result.SKUs) != 3 ||
		result.SKUs[0].Name != "DataZoneStandard" ||
		result.SKUs[1].Name != "GlobalStandard" {
		t.Fatalf("unexpected sorted SKUs: %+v", result.SKUs)
	}
	global := result.SKUs[1]
	if global.AvailableCapacity == nil || *global.AvailableCapacity != 120 {
		t.Fatalf("unexpected capacity: %+v", global)
	}
	if global.QuotaCurrent == nil || *global.QuotaCurrent != 20 ||
		global.QuotaLimit == nil || *global.QuotaLimit != 100 {
		t.Fatalf("unexpected quota: %+v", global)
	}
	if result.SKUs[2].AvailableCapacity != nil || result.SKUs[2].QuotaLimit != nil {
		t.Fatalf("expected unknown signals to remain unset: %+v", result.SKUs[2])
	}
	for _, sku := range result.SKUs {
		if sku.Name == "GlobalBatch" {
			t.Fatalf("batch SKU should not be offered for an online deployment: %+v", result.SKUs)
		}
	}
}

func TestScanModelAccountsTimesOutSlowAccount(t *testing.T) {
	account := &armcognitiveservices.Account{Name: new("slow-account")}
	start := time.Now()
	_, warnings := scanModelAccountsWithOptions(
		t.Context(),
		[]*armcognitiveservices.Account{account},
		1,
		10*time.Millisecond,
		func(ctx context.Context, _ *armcognitiveservices.Account) ([]ModelDeployment, []string) {
			<-ctx.Done()
			return nil, nil
		},
	)

	if time.Since(start) > time.Second {
		t.Fatal("account scan did not respect its timeout")
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "Timed out scanning Azure AI resource") {
		t.Fatalf("expected timeout warning, got %v", warnings)
	}
}

func TestModelAccountViewIncludesResourceGroupAndRegion(t *testing.T) {
	account := &armcognitiveservices.Account{
		ID:       new("/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/ai"),
		Name:     new("ai"),
		Kind:     new("AIServices"),
		Location: new("eastus2"),
	}

	view, ok := modelAccountView(account)
	if !ok {
		t.Fatal("expected account view")
	}
	if view.ResourceGroup != "rg" || view.Location != "eastus2" || view.Name != "ai" {
		t.Fatalf("unexpected account view: %+v", view)
	}
}

func accountID(account *armcognitiveservices.Account) string {
	if account.ID == nil {
		return ""
	}
	return *account.ID
}
