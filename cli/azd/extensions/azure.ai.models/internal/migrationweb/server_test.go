// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type staticProvider struct {
	result            ModelList
	assessment        TargetAssessment
	deploymentOptions DeploymentOptions
	metrics           DeploymentMetricsComparison
	err               error
}

func (p staticProvider) ListDeployments(context.Context) (ModelList, error) {
	return p.result, p.err
}

func (p staticProvider) ListResources(context.Context) (ModelResourceList, error) {
	return ModelResourceList{
		SubscriptionID: p.result.SubscriptionID,
		GeneratedAt:    p.result.GeneratedAt,
		Resources:      p.result.Accounts,
	}, p.err
}

func (p staticProvider) ListResourceDeployments(
	context.Context,
	ModelAccount,
) (ModelList, error) {
	return p.result, p.err
}

func (p staticProvider) AssessTarget(context.Context, AssessmentRequest) (TargetAssessment, error) {
	return p.assessment, p.err
}

func (p staticProvider) AssessDeploymentOptions(
	context.Context,
	DeploymentOptionsRequest,
) (DeploymentOptions, error) {
	return p.deploymentOptions, p.err
}

func (p staticProvider) QueryDeploymentMetrics(
	context.Context,
	DeploymentMetricsRequest,
) (DeploymentMetricsComparison, error) {
	return p.metrics, p.err
}

func TestServerServesAssetsAndProtectsAPI(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	server, err := NewServer(ServerOptions{
		SubscriptionID: "sub",
		Provider: staticProvider{
			result: ModelList{
				SubscriptionID: "sub",
				GeneratedAt:    time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC),
				Accounts: []ModelAccount{{
					Name:          "account",
					Kind:          "OpenAI",
					ResourceGroup: "rg",
					Location:      "eastus",
					ResourceID:    "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/account",
				}},
				Models: []ModelDeployment{{
					DeploymentName: "chat",
					ModelName:      "gpt-4o",
					ModelVersion:   "2024-05-13",
				}},
			},
			assessment: TargetAssessment{
				TargetModel:     "gpt-5.4",
				TargetVersion:   "2026-03-05",
				TargetFormat:    "OpenAI",
				LifecycleStatus: "GenerallyAvailable",
			},
			deploymentOptions: DeploymentOptions{
				TargetModel:   "gpt-5.4",
				TargetVersion: "2026-03-05",
				Region:        "eastus",
				SKUs: []DeploymentSKUOption{{
					Name:         "GlobalStandard",
					QuotaName:    "OpenAI.GlobalStandard.gpt-5.4",
					QuotaCurrent: new(float64(10)),
					QuotaLimit:   new(float64(100)),
				}},
			},
			metrics: DeploymentMetricsComparison{
				StartTime: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC),
				EndTime:   time.Date(2026, 9, 14, 1, 0, 0, 0, time.UTC),
				Source: DeploymentMetricSummary{
					DeploymentName: "chat",
					Requests:       new(float64(42)),
				},
				Target: DeploymentMetricSummary{
					DeploymentName: "chat-next",
					Requests:       new(float64(40)),
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx)
	}()

	pageResponse, err := http.Get(server.BrowserURL())
	if err != nil {
		t.Fatal(err)
	}
	page, err := io.ReadAll(pageResponse.Body)
	_ = pageResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if pageResponse.StatusCode != http.StatusOK || !strings.Contains(string(page), "Model Migration") {
		t.Fatalf("unexpected page response: status=%d body=%q", pageResponse.StatusCode, string(page))
	}

	unauthorized, err := http.Get(server.URL() + "/api/models")
	if err != nil {
		t.Fatal(err)
	}
	_ = unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unexpected unauthorized status: %d", unauthorized.StatusCode)
	}

	browserURL, err := url.Parse(server.BrowserURL())
	if err != nil {
		t.Fatal(err)
	}
	resourcesRequest, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		server.URL()+"/api/resources",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	resourcesRequest.Header.Set("Authorization", "Bearer "+browserURL.Query().Get("token"))
	resourcesResponse, err := http.DefaultClient.Do(resourcesRequest)
	if err != nil {
		t.Fatal(err)
	}
	var resources ModelResourceList
	if err := json.NewDecoder(resourcesResponse.Body).Decode(&resources); err != nil {
		t.Fatal(err)
	}
	_ = resourcesResponse.Body.Close()
	if resourcesResponse.StatusCode != http.StatusOK ||
		len(resources.Resources) != 1 ||
		resources.Resources[0].Name != "account" {
		t.Fatalf("unexpected resource response: status=%d payload=%+v", resourcesResponse.StatusCode, resources)
	}

	resourceModelsRequest, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		server.URL()+"/api/resource-models",
		bytes.NewBufferString(
			`{"name":"account","kind":"OpenAI","resourceGroup":"rg","location":"eastus",`+
				`"resourceId":"/subscriptions/sub/resourceGroups/rg/providers/`+
				`Microsoft.CognitiveServices/accounts/account"}`,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	resourceModelsRequest.Header.Set("Authorization", "Bearer "+browserURL.Query().Get("token"))
	resourceModelsResponse, err := http.DefaultClient.Do(resourceModelsRequest)
	if err != nil {
		t.Fatal(err)
	}
	var resourceModels ModelList
	if err := json.NewDecoder(resourceModelsResponse.Body).Decode(&resourceModels); err != nil {
		t.Fatal(err)
	}
	_ = resourceModelsResponse.Body.Close()
	if resourceModelsResponse.StatusCode != http.StatusOK ||
		len(resourceModels.Models) != 1 ||
		resourceModels.Models[0].DeploymentName != "chat" {
		t.Fatalf(
			"unexpected resource models response: status=%d payload=%+v",
			resourceModelsResponse.StatusCode,
			resourceModels,
		)
	}

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL()+"/api/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+browserURL.Query().Get("token"))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected API status: %d", response.StatusCode)
	}
	var payload ModelList
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if len(payload.Models) != 1 || payload.Models[0].DeploymentName != "chat" {
		t.Fatalf("unexpected API payload: %+v", payload)
	}

	recommendationRequest, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		server.URL()+"/api/recommendations",
		bytes.NewBufferString(`{"modelName":"gpt-4o","modelVersion":"2024-05-13"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	recommendationRequest.Header.Set("Authorization", "Bearer "+browserURL.Query().Get("token"))
	recommendationResponse, err := http.DefaultClient.Do(recommendationRequest)
	if err != nil {
		t.Fatal(err)
	}
	if recommendationResponse.StatusCode != http.StatusOK {
		t.Fatalf("unexpected recommendation status: %d", recommendationResponse.StatusCode)
	}
	var recommendation ReplacementRecommendation
	if err := json.NewDecoder(recommendationResponse.Body).Decode(&recommendation); err != nil {
		t.Fatal(err)
	}
	_ = recommendationResponse.Body.Close()
	if recommendation.SourceModel != "gpt-4o" ||
		recommendation.SuggestedModel != "gpt-5.4" ||
		recommendation.SuggestedFormat != "OpenAI" ||
		recommendation.Source != "default" {
		t.Fatalf("unexpected recommendation: %+v", recommendation)
	}

	invalidAssessmentRequest, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		server.URL()+"/api/assessments",
		bytes.NewBufferString(`{"targetModel":"gpt-5.4"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	invalidAssessmentRequest.Header.Set("Authorization", "Bearer "+browserURL.Query().Get("token"))
	invalidAssessmentResponse, err := http.DefaultClient.Do(invalidAssessmentRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = invalidAssessmentResponse.Body.Close()
	if invalidAssessmentResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected invalid assessment status: %d", invalidAssessmentResponse.StatusCode)
	}

	assessmentRequest, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		server.URL()+"/api/assessments",
		bytes.NewBufferString(
			`{"resourceGroup":"rg","accountName":"account","location":"eastus",`+
				`"targetModel":"gpt-5.4","targetFormat":"OpenAI"}`,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	assessmentRequest.Header.Set("Authorization", "Bearer "+browserURL.Query().Get("token"))
	assessmentResponse, err := http.DefaultClient.Do(assessmentRequest)
	if err != nil {
		t.Fatal(err)
	}
	if assessmentResponse.StatusCode != http.StatusOK {
		t.Fatalf("unexpected assessment status: %d", assessmentResponse.StatusCode)
	}
	var assessment TargetAssessment
	if err := json.NewDecoder(assessmentResponse.Body).Decode(&assessment); err != nil {
		t.Fatal(err)
	}
	_ = assessmentResponse.Body.Close()
	if assessment.TargetModel != "gpt-5.4" ||
		assessment.TargetVersion != "2026-03-05" {
		t.Fatalf("unexpected assessment: %+v", assessment)
	}

	deploymentOptionsRequest, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		server.URL()+"/api/deployment-options",
		bytes.NewBufferString(
			`{"resourceGroup":"rg","accountName":"account","region":"eastus",`+
				`"targetModel":"gpt-5.4","targetFormat":"OpenAI"}`,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	deploymentOptionsRequest.Header.Set("Authorization", "Bearer "+browserURL.Query().Get("token"))
	deploymentOptionsResponse, err := http.DefaultClient.Do(deploymentOptionsRequest)
	if err != nil {
		t.Fatal(err)
	}
	if deploymentOptionsResponse.StatusCode != http.StatusOK {
		t.Fatalf("unexpected deployment options status: %d", deploymentOptionsResponse.StatusCode)
	}
	var deploymentOptions DeploymentOptions
	if err := json.NewDecoder(deploymentOptionsResponse.Body).Decode(&deploymentOptions); err != nil {
		t.Fatal(err)
	}
	_ = deploymentOptionsResponse.Body.Close()
	if deploymentOptions.Region != "eastus" ||
		len(deploymentOptions.SKUs) != 1 ||
		deploymentOptions.SKUs[0].Name != "GlobalStandard" {
		t.Fatalf("unexpected deployment options: %+v", deploymentOptions)
	}

	invalidDeploymentOptionsRequest, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		server.URL()+"/api/deployment-options",
		bytes.NewBufferString(`{"resourceGroup":"rg"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	invalidDeploymentOptionsRequest.Header.Set(
		"Authorization",
		"Bearer "+browserURL.Query().Get("token"),
	)
	invalidDeploymentOptionsResponse, err := http.DefaultClient.Do(invalidDeploymentOptionsRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = invalidDeploymentOptionsResponse.Body.Close()
	if invalidDeploymentOptionsResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf(
			"unexpected invalid deployment options status: %d",
			invalidDeploymentOptionsResponse.StatusCode,
		)
	}

	metricsRequest, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		server.URL()+"/api/deployment-metrics",
		bytes.NewBufferString(
			`{"source":{"resourceId":"/subscriptions/sub/resourceGroups/rg/providers/`+
				`Microsoft.CognitiveServices/accounts/account","location":"eastus",`+
				`"deploymentName":"chat"},"target":{"resourceId":"/subscriptions/sub/`+
				`resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/account",`+
				`"location":"eastus","deploymentName":"chat-next"},`+
				`"startTime":"2026-09-14T00:00:00Z","endTime":"2026-09-14T01:00:00Z"}`,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	metricsRequest.Header.Set("Authorization", "Bearer "+browserURL.Query().Get("token"))
	metricsResponse, err := http.DefaultClient.Do(metricsRequest)
	if err != nil {
		t.Fatal(err)
	}
	var metrics DeploymentMetricsComparison
	if err := json.NewDecoder(metricsResponse.Body).Decode(&metrics); err != nil {
		t.Fatal(err)
	}
	_ = metricsResponse.Body.Close()
	if metricsResponse.StatusCode != http.StatusOK ||
		metrics.Source.Requests == nil ||
		*metrics.Source.Requests != 42 {
		t.Fatalf(
			"unexpected deployment metrics response: status=%d payload=%+v",
			metricsResponse.StatusCode,
			metrics,
		)
	}

	http.DefaultClient.CloseIdleConnections()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestModelListSerializesEmptyModelsAsArray(t *testing.T) {
	resource := ModelAccount{
		Name:          "account",
		Kind:          "OpenAI",
		ResourceGroup: "rg",
		Location:      "eastus",
		ResourceID:    "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/account",
	}

	// Empty model results must serialize as [] so progressive loading can append safely.
	result := ModelList{
		SubscriptionID: "sub",
		GeneratedAt:    time.Now(),
		Accounts:       []ModelAccount{resource},
		Models:         []ModelDeployment{},
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(payload, []byte(`"models":[]`)) {
		t.Fatalf("expected empty models array, got %s", payload)
	}
}
