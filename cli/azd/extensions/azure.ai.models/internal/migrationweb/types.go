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
	Accounts       []ModelAccount    `json:"accounts"`
	Models         []ModelDeployment `json:"models"`
	Warnings       []string          `json:"warnings,omitempty"`
}

type ModelResourceList struct {
	SubscriptionID string         `json:"subscriptionId"`
	GeneratedAt    time.Time      `json:"generatedAt"`
	Resources      []ModelAccount `json:"resources"`
}

type ModelAccount struct {
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	ResourceGroup string `json:"resourceGroup"`
	Location      string `json:"location"`
	ResourceID    string `json:"resourceId"`
}

type ReplacementRequest struct {
	ModelName    string `json:"modelName"`
	ModelVersion string `json:"modelVersion,omitempty"`
	ModelFormat  string `json:"modelFormat,omitempty"`
}

type ReplacementRecommendation struct {
	SourceModel     string `json:"sourceModel"`
	SuggestedModel  string `json:"suggestedModel"`
	SuggestedFormat string `json:"suggestedFormat"`
	Source          string `json:"source"`
}

type AssessmentRequest struct {
	ResourceGroup string `json:"resourceGroup"`
	AccountName   string `json:"accountName"`
	TargetModel   string `json:"targetModel"`
	TargetVersion string `json:"targetVersion,omitempty"`
	TargetFormat  string `json:"targetFormat"`
}

type ModelCapability struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type TargetAssessment struct {
	TargetModel     string            `json:"targetModel"`
	TargetVersion   string            `json:"targetVersion,omitempty"`
	TargetFormat    string            `json:"targetFormat,omitempty"`
	Publisher       string            `json:"publisher,omitempty"`
	LifecycleStatus string            `json:"lifecycleStatus,omitempty"`
	RetirementDate  *string           `json:"retirementDate,omitempty"`
	Capabilities    []ModelCapability `json:"capabilities"`
	Warnings        []string          `json:"warnings,omitempty"`
}

type DeploymentOptionsRequest struct {
	ResourceGroup string `json:"resourceGroup"`
	AccountName   string `json:"accountName"`
	Region        string `json:"region"`
	TargetModel   string `json:"targetModel"`
	TargetFormat  string `json:"targetFormat"`
}

type DeploymentSKUOption struct {
	Name              string   `json:"name"`
	AvailableCapacity *float32 `json:"availableCapacity,omitempty"`
	QuotaName         string   `json:"quotaName,omitempty"`
	QuotaCurrent      *float64 `json:"quotaCurrent,omitempty"`
	QuotaLimit        *float64 `json:"quotaLimit,omitempty"`
}

type DeploymentOptions struct {
	TargetModel   string                `json:"targetModel"`
	TargetVersion string                `json:"targetVersion,omitempty"`
	Region        string                `json:"region"`
	SKUs          []DeploymentSKUOption `json:"skus"`
	Warnings      []string              `json:"warnings,omitempty"`
}

type ModelProvider interface {
	ListResources(ctx context.Context) (ModelResourceList, error)
	ListResourceDeployments(ctx context.Context, resource ModelAccount) (ModelList, error)
	ListDeployments(ctx context.Context) (ModelList, error)
	AssessTarget(ctx context.Context, request AssessmentRequest) (TargetAssessment, error)
	AssessDeploymentOptions(ctx context.Context, request DeploymentOptionsRequest) (DeploymentOptions, error)
}
