// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cognitiveservices/armcognitiveservices/v2"
)

type AzureModelProvider struct {
	subscriptionID string
	accounts       *armcognitiveservices.AccountsClient
	deployments    *armcognitiveservices.DeploymentsClient
}

func NewAzureModelProvider(
	subscriptionID string,
	credential azcore.TokenCredential,
) (*AzureModelProvider, error) {
	factory, err := armcognitiveservices.NewClientFactory(subscriptionID, credential, nil)
	if err != nil {
		return nil, fmt.Errorf("create Cognitive Services client: %w", err)
	}
	return &AzureModelProvider{
		subscriptionID: subscriptionID,
		accounts:       factory.NewAccountsClient(),
		deployments:    factory.NewDeploymentsClient(),
	}, nil
}

func (p *AzureModelProvider) ListDeployments(ctx context.Context) (ModelList, error) {
	result := ModelList{
		SubscriptionID: p.subscriptionID,
		GeneratedAt:    time.Now().UTC(),
		Models:         []ModelDeployment{},
	}

	accountPager := p.accounts.NewListPager(nil)
	for accountPager.More() {
		page, err := accountPager.NextPage(ctx)
		if err != nil {
			return ModelList{}, fmt.Errorf("list Cognitive Services accounts: %w", err)
		}
		for _, account := range page.Value {
			if !isModelAccount(account) {
				continue
			}
			models, warnings := p.listAccountDeployments(ctx, account)
			result.Models = append(result.Models, models...)
			result.Warnings = append(result.Warnings, warnings...)
		}
	}

	slices.SortFunc(result.Models, func(left, right ModelDeployment) int {
		if left.AccountName != right.AccountName {
			return cmp.Compare(left.AccountName, right.AccountName)
		}
		return cmp.Compare(left.DeploymentName, right.DeploymentName)
	})
	slices.Sort(result.Warnings)
	return result, nil
}

func isModelAccount(account *armcognitiveservices.Account) bool {
	if account == nil || account.Kind == nil {
		return false
	}
	return strings.EqualFold(*account.Kind, "OpenAI") || strings.EqualFold(*account.Kind, "AIServices")
}

func (p *AzureModelProvider) listAccountDeployments(
	ctx context.Context,
	account *armcognitiveservices.Account,
) ([]ModelDeployment, []string) {
	if account.ID == nil || account.Name == nil {
		return nil, []string{"Skipped a Cognitive Services account with missing resource identity."}
	}
	resourceID, err := arm.ParseResourceID(*account.ID)
	if err != nil {
		return nil, []string{fmt.Sprintf("Skipped account %q: invalid resource ID.", value(account.Name))}
	}

	accountModels, err := p.listAccountModels(ctx, resourceID.ResourceGroupName, *account.Name)
	var warnings []string
	if err != nil {
		warnings = append(warnings, fmt.Sprintf(
			"Could not read lifecycle metadata for account %q: %v",
			*account.Name,
			err,
		))
		accountModels = map[string]*armcognitiveservices.AccountModel{}
	}

	deploymentPager := p.deployments.NewListPager(resourceID.ResourceGroupName, *account.Name, nil)
	var models []ModelDeployment
	for deploymentPager.More() {
		page, err := deploymentPager.NextPage(ctx)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf(
				"Could not list deployments for account %q: %v",
				*account.Name,
				err,
			))
			break
		}
		for _, deployment := range page.Value {
			models = append(models, deploymentView(account, resourceID.ResourceGroupName, deployment, accountModels))
		}
	}
	return models, warnings
}

func (p *AzureModelProvider) listAccountModels(
	ctx context.Context,
	resourceGroup string,
	accountName string,
) (map[string]*armcognitiveservices.AccountModel, error) {
	models := make(map[string]*armcognitiveservices.AccountModel)
	pager := p.accounts.NewListModelsPager(resourceGroup, accountName, nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, model := range page.Value {
			if model == nil {
				continue
			}
			models[modelKey(value(model.Format), value(model.Name), value(model.Version))] = model
		}
	}
	return models, nil
}

func deploymentView(
	account *armcognitiveservices.Account,
	resourceGroup string,
	deployment *armcognitiveservices.Deployment,
	accountModels map[string]*armcognitiveservices.AccountModel,
) ModelDeployment {
	if deployment == nil {
		return ModelDeployment{
			AccountName:   value(account.Name),
			AccountKind:   value(account.Kind),
			ResourceGroup: resourceGroup,
			Location:      value(account.Location),
		}
	}
	view := ModelDeployment{
		DeploymentName: value(deployment.Name),
		AccountName:    value(account.Name),
		AccountKind:    value(account.Kind),
		ResourceGroup:  resourceGroup,
		Location:       value(account.Location),
		ResourceID:     value(deployment.ID),
	}
	if deployment.Properties == nil || deployment.Properties.Model == nil {
		return view
	}

	model := deployment.Properties.Model
	view.ModelName = value(model.Name)
	view.ModelVersion = value(model.Version)
	view.ModelFormat = value(model.Format)
	view.VersionUpgradeOption = stringValue(deployment.Properties.VersionUpgradeOption)

	catalogModel := accountModels[modelKey(view.ModelFormat, view.ModelName, view.ModelVersion)]
	if catalogModel == nil {
		return view
	}
	view.LifecycleStatus = stringValue(catalogModel.LifecycleStatus)
	if catalogModel.Deprecation != nil && catalogModel.Deprecation.Inference != nil {
		retirementDate := *catalogModel.Deprecation.Inference
		view.RetirementDate = &retirementDate
	}
	return view
}

func modelKey(format string, name string, version string) string {
	return strings.ToLower(strings.Join([]string{format, name, version}, "\x00"))
}

func value(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringValue[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}
