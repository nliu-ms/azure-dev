// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"context"
	"time"
)

type ModelDeployment struct {
	DeploymentName       string  `json:"deploymentName"`
	ModelName            string  `json:"modelName"`
	ModelVersion         string  `json:"modelVersion"`
	ModelFormat          string  `json:"modelFormat"`
	LifecycleStatus      string  `json:"lifecycleStatus"`
	RetirementDate       *string `json:"retirementDate,omitempty"`
	VersionUpgradeOption string  `json:"versionUpgradeOption,omitempty"`
	AccountName          string  `json:"accountName"`
	AccountKind          string  `json:"accountKind"`
	ResourceGroup        string  `json:"resourceGroup"`
	Location             string  `json:"location"`
	ResourceID           string  `json:"resourceId"`
}

type ModelList struct {
	SubscriptionID string            `json:"subscriptionId"`
	GeneratedAt    time.Time         `json:"generatedAt"`
	Models         []ModelDeployment `json:"models"`
	Warnings       []string          `json:"warnings,omitempty"`
}

type ModelProvider interface {
	ListDeployments(ctx context.Context) (ModelList, error)
}
