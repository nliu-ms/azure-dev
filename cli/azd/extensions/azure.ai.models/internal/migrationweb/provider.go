// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cognitiveservices/armcognitiveservices/v2"
)

const (
	maxConcurrentAccountScans = 6
	accountScanTimeout        = 15 * time.Second
)

type AzureModelProvider struct {
	subscriptionID string
	accounts       *armcognitiveservices.AccountsClient
	deployments    *armcognitiveservices.DeploymentsClient
	models         *armcognitiveservices.ModelsClient
	capacities     *armcognitiveservices.LocationBasedModelCapacitiesClient
	usages         *armcognitiveservices.UsagesClient
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
		models:         factory.NewModelsClient(),
		capacities:     factory.NewLocationBasedModelCapacitiesClient(),
		usages:         factory.NewUsagesClient(),
	}, nil
}

func (p *AzureModelProvider) ListResources(ctx context.Context) (ModelResourceList, error) {
	result := ModelResourceList{
		SubscriptionID: p.subscriptionID,
		GeneratedAt:    time.Now().UTC(),
		Resources:      []ModelAccount{},
	}
	accountPager := p.accounts.NewListPager(nil)
	for accountPager.More() {
		page, err := accountPager.NextPage(ctx)
		if err != nil {
			return ModelResourceList{}, fmt.Errorf("list Azure AI resources: %w", err)
		}
		for _, account := range page.Value {
			if !isModelAccount(account) {
				continue
			}
			if view, ok := modelAccountView(account); ok {
				result.Resources = append(result.Resources, view)
			}
		}
	}
	slices.SortFunc(result.Resources, func(left, right ModelAccount) int {
		if left.Name != right.Name {
			return cmp.Compare(left.Name, right.Name)
		}
		return cmp.Compare(left.ResourceGroup, right.ResourceGroup)
	})
	return result, nil
}

func (p *AzureModelProvider) ListResourceDeployments(
	ctx context.Context,
	resource ModelAccount,
) (ModelList, error) {
	account := &armcognitiveservices.Account{
		ID:       &resource.ResourceID,
		Name:     &resource.Name,
		Kind:     &resource.Kind,
		Location: &resource.Location,
	}
	models, warnings := p.listAccountDeployments(ctx, account)
	if models == nil {
		models = []ModelDeployment{}
	}
	return ModelList{
		SubscriptionID: p.subscriptionID,
		GeneratedAt:    time.Now().UTC(),
		Accounts:       []ModelAccount{resource},
		Models:         models,
		Warnings:       warnings,
	}, nil
}

func (p *AzureModelProvider) ListDeployments(ctx context.Context) (ModelList, error) {
	result := ModelList{
		SubscriptionID: p.subscriptionID,
		GeneratedAt:    time.Now().UTC(),
		Accounts:       []ModelAccount{},
		Models:         []ModelDeployment{},
	}

	var accounts []*armcognitiveservices.Account
	accountPager := p.accounts.NewListPager(nil)
	for accountPager.More() {
		page, err := accountPager.NextPage(ctx)
		if err != nil {
			if len(accounts) == 0 {
				return ModelList{}, fmt.Errorf("list Azure AI resources: %w", err)
			}
			result.Warnings = append(
				result.Warnings,
				fmt.Sprintf("Azure AI resource discovery stopped early: %v", err),
			)
			break
		}
		for _, account := range page.Value {
			if isModelAccount(account) {
				accounts = append(accounts, account)
				if view, ok := modelAccountView(account); ok {
					result.Accounts = append(result.Accounts, view)
				}
			}
		}
	}

	models, warnings := scanModelAccounts(ctx, accounts, p.listAccountDeployments)
	result.Models = append(result.Models, models...)
	result.Warnings = append(result.Warnings, warnings...)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		result.Warnings = append(
			result.Warnings,
			"Inventory scanning reached its time limit. Results may be incomplete.",
		)
	}

	slices.SortFunc(result.Models, func(left, right ModelDeployment) int {
		if left.AccountName != right.AccountName {
			return cmp.Compare(left.AccountName, right.AccountName)
		}
		return cmp.Compare(left.DeploymentName, right.DeploymentName)
	})
	slices.SortFunc(result.Accounts, func(left, right ModelAccount) int {
		if left.Name != right.Name {
			return cmp.Compare(left.Name, right.Name)
		}
		return cmp.Compare(left.ResourceGroup, right.ResourceGroup)
	})
	slices.Sort(result.Warnings)
	return result, nil
}

func modelAccountView(account *armcognitiveservices.Account) (ModelAccount, bool) {
	if account == nil || account.ID == nil || account.Name == nil {
		return ModelAccount{}, false
	}
	resourceID, err := arm.ParseResourceID(*account.ID)
	if err != nil {
		return ModelAccount{}, false
	}
	return ModelAccount{
		Name:          *account.Name,
		Kind:          value(account.Kind),
		ResourceGroup: resourceID.ResourceGroupName,
		Location:      value(account.Location),
		ResourceID:    *account.ID,
	}, true
}

type accountScanner func(
	context.Context,
	*armcognitiveservices.Account,
) ([]ModelDeployment, []string)

func scanModelAccounts(
	ctx context.Context,
	accounts []*armcognitiveservices.Account,
	scan accountScanner,
) ([]ModelDeployment, []string) {
	return scanModelAccountsWithOptions(
		ctx,
		accounts,
		maxConcurrentAccountScans,
		accountScanTimeout,
		scan,
	)
}

func scanModelAccountsWithOptions(
	ctx context.Context,
	accounts []*armcognitiveservices.Account,
	maxConcurrent int,
	timeout time.Duration,
	scan accountScanner,
) ([]ModelDeployment, []string) {
	var (
		models   []ModelDeployment
		warnings []string
		mutex    sync.Mutex
		wait     sync.WaitGroup
		limit    = make(chan struct{}, maxConcurrent)
	)

	for _, account := range accounts {
		if ctx.Err() != nil {
			break
		}
		wait.Add(1)
		go func(account *armcognitiveservices.Account) {
			defer wait.Done()
			select {
			case limit <- struct{}{}:
				defer func() { <-limit }()
			case <-ctx.Done():
				return
			}

			accountCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			accountModels, accountWarnings := scan(accountCtx, account)
			if errors.Is(accountCtx.Err(), context.DeadlineExceeded) {
				accountWarnings = append(
					accountWarnings,
					fmt.Sprintf(
						"Timed out scanning Azure AI resource %q after %s.",
						value(account.Name),
						timeout,
					),
				)
			}

			mutex.Lock()
			models = append(models, accountModels...)
			warnings = append(warnings, accountWarnings...)
			mutex.Unlock()
		}(account)
	}
	wait.Wait()
	return models, warnings
}

func (p *AzureModelProvider) AssessTarget(
	ctx context.Context,
	request AssessmentRequest,
) (TargetAssessment, error) {
	result := TargetAssessment{
		TargetModel:  request.TargetModel,
		TargetFormat: request.TargetFormat,
		Capabilities: []ModelCapability{},
	}

	models, err := p.listAccountModels(ctx, request.ResourceGroup, request.AccountName)
	if err != nil {
		return TargetAssessment{}, fmt.Errorf(
			"list target models for Azure AI resource %q: %w",
			request.AccountName,
			err,
		)
	}
	target := selectTargetModel(models, request.TargetFormat, request.TargetModel, request.TargetVersion)
	if target == nil {
		result.Warnings = append(
			result.Warnings,
			fmt.Sprintf(
				"%s is not currently returned for Azure AI resource %q.",
				request.TargetModel,
				request.AccountName,
			),
		)
		return result, nil
	}

	return targetAssessment(result, target), nil
}

func (p *AzureModelProvider) AssessDeploymentOptions(
	ctx context.Context,
	request DeploymentOptionsRequest,
) (DeploymentOptions, error) {
	result := DeploymentOptions{
		TargetModel: request.TargetModel,
		Region:      request.Region,
		SKUs:        []DeploymentSKUOption{},
	}
	models, err := p.listLocationModels(ctx, request.Region)
	if err != nil {
		return DeploymentOptions{}, fmt.Errorf(
			"list target models in region %q: %w",
			request.Region,
			err,
		)
	}
	target := selectTargetModel(models, request.TargetFormat, request.TargetModel, "")
	if target == nil {
		result.Warnings = append(
			result.Warnings,
			fmt.Sprintf("%s model metadata is unavailable.", request.TargetModel),
		)
		return result, nil
	}
	result.TargetVersion = value(target.Version)

	capacities, err := p.listTargetCapacities(
		ctx,
		request.Region,
		value(target.Format),
		value(target.Name),
		value(target.Version),
	)
	if err != nil {
		result.Warnings = append(
			result.Warnings,
			fmt.Sprintf("Could not check model availability in %s: %v", request.Region, err),
		)
	}
	usages, err := p.listUsages(ctx, request.Region)
	if err != nil {
		result.Warnings = append(
			result.Warnings,
			fmt.Sprintf("Could not check quota in %s: %v", request.Region, err),
		)
	}

	return deploymentOptionsForModel(result, target, capacities, usages), nil
}

func deploymentOptionsForModel(
	result DeploymentOptions,
	target *armcognitiveservices.AccountModel,
	capacities map[string]*float32,
	usages map[string]*armcognitiveservices.Usage,
) DeploymentOptions {
	for _, sku := range target.SKUs {
		if sku == nil || sku.Name == nil {
			continue
		}
		if strings.HasSuffix(strings.ToLower(*sku.Name), "batch") {
			continue
		}
		option := DeploymentSKUOption{
			Name:      *sku.Name,
			QuotaName: value(sku.UsageName),
		}
		option.AvailableCapacity = capacities[strings.ToLower(option.Name)]
		if usage := usages[strings.ToLower(option.QuotaName)]; usage != nil {
			option.QuotaCurrent = usage.CurrentValue
			option.QuotaLimit = usage.Limit
		}
		result.SKUs = append(result.SKUs, option)
	}
	slices.SortFunc(result.SKUs, func(left, right DeploymentSKUOption) int {
		return cmp.Compare(left.Name, right.Name)
	})
	return result
}

func (p *AzureModelProvider) listLocationModels(
	ctx context.Context,
	location string,
) (map[string]*armcognitiveservices.AccountModel, error) {
	result := make(map[string]*armcognitiveservices.AccountModel)
	pager := p.models.NewListPager(location, nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Value {
			if item == nil || item.Model == nil {
				continue
			}
			result[modelKey(
				value(item.Model.Format),
				value(item.Model.Name),
				value(item.Model.Version),
			)] = item.Model
		}
	}
	return result, nil
}

func (p *AzureModelProvider) listTargetCapacities(
	ctx context.Context,
	location string,
	format string,
	name string,
	version string,
) (map[string]*float32, error) {
	result := make(map[string]*float32)
	pager := p.capacities.NewListPager(location, format, name, version, nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Value {
			if item == nil || item.Properties == nil || item.Properties.SKUName == nil {
				continue
			}
			result[strings.ToLower(*item.Properties.SKUName)] = item.Properties.AvailableCapacity
		}
	}
	return result, nil
}

func (p *AzureModelProvider) listUsages(
	ctx context.Context,
	location string,
) (map[string]*armcognitiveservices.Usage, error) {
	result := make(map[string]*armcognitiveservices.Usage)
	pager := p.usages.NewListPager(location, nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, usage := range page.Value {
			if usage == nil || usage.Name == nil || usage.Name.Value == nil {
				continue
			}
			result[strings.ToLower(*usage.Name.Value)] = usage
		}
	}
	return result, nil
}

func isModelAccount(account *armcognitiveservices.Account) bool {
	if account == nil || account.Kind == nil {
		return false
	}
	return strings.EqualFold(*account.Kind, "OpenAI") || strings.EqualFold(*account.Kind, "AIServices")
}

func selectTargetModel(
	models map[string]*armcognitiveservices.AccountModel,
	format string,
	name string,
	version string,
) *armcognitiveservices.AccountModel {
	var selected *armcognitiveservices.AccountModel
	for _, model := range models {
		if model == nil ||
			!strings.EqualFold(value(model.Format), format) ||
			!strings.EqualFold(value(model.Name), name) {
			continue
		}
		if version != "" {
			if value(model.Version) == version {
				return model
			}
			continue
		}
		if model.IsDefaultVersion != nil && *model.IsDefaultVersion {
			return model
		}
		if selected == nil || value(model.Version) > value(selected.Version) {
			selected = model
		}
	}
	return selected
}

func targetAssessment(
	result TargetAssessment,
	model *armcognitiveservices.AccountModel,
) TargetAssessment {
	result.TargetModel = value(model.Name)
	result.TargetVersion = value(model.Version)
	result.TargetFormat = value(model.Format)
	result.Publisher = value(model.Publisher)
	result.LifecycleStatus = stringValue(model.LifecycleStatus)
	if model.Deprecation != nil && model.Deprecation.Inference != nil {
		retirementDate := *model.Deprecation.Inference
		result.RetirementDate = &retirementDate
	}

	result.Capabilities = result.Capabilities[:0]
	for name, capability := range model.Capabilities {
		result.Capabilities = append(result.Capabilities, ModelCapability{
			Name:  name,
			Value: value(capability),
		})
	}
	slices.SortFunc(result.Capabilities, func(left, right ModelCapability) int {
		return cmp.Compare(left.Name, right.Name)
	})

	return result
}

func (p *AzureModelProvider) listAccountDeployments(
	ctx context.Context,
	account *armcognitiveservices.Account,
) ([]ModelDeployment, []string) {
	if account.ID == nil || account.Name == nil {
		return nil, []string{"Skipped an Azure AI resource with missing resource identity."}
	}
	resourceID, err := arm.ParseResourceID(*account.ID)
	if err != nil {
		return nil, []string{fmt.Sprintf(
			"Skipped Azure AI resource %q: invalid resource ID.",
			value(account.Name),
		)}
	}

	accountModels, err := p.listAccountModels(ctx, resourceID.ResourceGroupName, *account.Name)
	var warnings []string
	if err != nil {
		warnings = append(warnings, fmt.Sprintf(
			"Could not read lifecycle metadata for Azure AI resource %q: %v",
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
				"Could not list deployments for Azure AI resource %q: %v",
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
