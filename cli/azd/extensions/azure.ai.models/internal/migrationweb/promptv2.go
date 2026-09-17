// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

const (
	promptV2Scope               = "https://ai.azure.com/.default"
	promptV2CompatibilityTarget = "gpt-5.2"
	maxPromptBytes              = 256 * 1024
	maxPromptV2ResponseBytes    = 4 * 1024 * 1024
	maxOptimizationCases        = 20
	maxOptimizationFieldLength  = 2_000
)

var azureOpenAIAccountName = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,62}[A-Za-z0-9])?$`)

// PromptOptimizationInput contains the prompt, model, and regression evidence sent to PromptV2.
type PromptOptimizationInput struct {
	SourcePrompt        string
	SourceModel         string
	TargetModel         string
	OptimizerAccount    string
	OptimizerModel      string
	OptimizerDeployment string
	RequestedChanges    string
	VerificationCaseIDs []string
}

// PromptOptimizerComment explains one PromptV2 change and its source location.
type PromptOptimizerComment struct {
	Kind     string                         `json:"kind"`
	Location PromptOptimizerCommentLocation `json:"location"`
	Reason   string                         `json:"reason"`
}

// PromptOptimizerCommentLocation identifies the before and after line anchors for a change.
type PromptOptimizerCommentLocation struct {
	Before PromptOptimizerPosition `json:"before"`
	After  PromptOptimizerPosition `json:"after"`
}

// PromptOptimizerPosition identifies a line and column in a PromptV2 prompt.
type PromptOptimizerPosition struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// PromptOptimizationResult contains the PromptV2 candidate and its evidence traceability.
type PromptOptimizationResult struct {
	SourcePrompt        string                   `json:"sourcePrompt"`
	OptimizedPrompt     string                   `json:"optimizedPrompt"`
	RequestedChanges    string                   `json:"requestedChanges"`
	Comments            []PromptOptimizerComment `json:"comments"`
	VerificationCaseIDs []string                 `json:"verificationCaseIds"`
	OptimizerModel      string                   `json:"optimizerModel"`
	OptimizerDeployment string                   `json:"optimizerDeployment"`
	TargetSpecific      bool                     `json:"targetSpecific"`
	PromptSHA256        string                   `json:"promptSha256"`
	EvaluationSHA256    string                   `json:"evaluationSha256"`
}

// PromptOptimizer generates an evidence-backed prompt candidate.
type PromptOptimizer interface {
	Optimize(ctx context.Context, input PromptOptimizationInput) (PromptOptimizationResult, error)
}

// PromptV2Error reports an unsuccessful PromptV2 HTTP response.
type PromptV2Error struct {
	StatusCode int
	Message    string
}

func (e *PromptV2Error) Error() string {
	return e.Message
}

type promptV2Client struct {
	credential         azcore.TokenCredential
	httpClient         *http.Client
	endpointForAccount func(string) string
}

type promptV2Request struct {
	DeveloperMessage    string `json:"developer_message"`
	Messages            []any  `json:"messages"`
	ModelName           string `json:"model_name"`
	ModelDeploymentName string `json:"model_deployment_name"`
	OptimizingFor       string `json:"optimizing_for"`
	RequestedChanges    string `json:"requested_changes"`
	Tools               []any  `json:"tools"`
}

type promptV2Response struct {
	Comments            []PromptOptimizerComment `json:"comments"`
	NewDeveloperMessage string                   `json:"new_developer_message"`
}

// NewPromptV2Client creates a PromptV2 client using the azd Azure credential.
func NewPromptV2Client(credential azcore.TokenCredential) (PromptOptimizer, error) {
	if credential == nil {
		return nil, errors.New("PromptV2 credential is required")
	}
	return &promptV2Client{
		credential: credential,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		endpointForAccount: func(account string) string {
			return fmt.Sprintf(
				"https://%s.openai.azure.com/openai/v1/dashboard/generate/optimize/promptv2",
				account,
			)
		},
	}, nil
}

func (c *promptV2Client) Optimize(
	ctx context.Context,
	input PromptOptimizationInput,
) (PromptOptimizationResult, error) {
	if !azureOpenAIAccountName.MatchString(input.OptimizerAccount) {
		return PromptOptimizationResult{}, errors.New("optimizer Azure OpenAI account name is invalid")
	}
	if strings.TrimSpace(input.OptimizerModel) == "" || strings.TrimSpace(input.OptimizerDeployment) == "" {
		return PromptOptimizationResult{}, errors.New("optimizer model and deployment are required")
	}
	if strings.TrimSpace(input.SourcePrompt) == "" {
		return PromptOptimizationResult{}, errors.New("Source prompt is empty")
	}
	if len(input.SourcePrompt) > maxPromptBytes {
		return PromptOptimizationResult{}, fmt.Errorf("Source prompt exceeds the %d KB limit", maxPromptBytes/1024)
	}
	if strings.TrimSpace(input.RequestedChanges) == "" {
		return PromptOptimizationResult{}, errors.New("PromptV2 requires prompt-fixable regression evidence")
	}

	token, err := c.credential.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{promptV2Scope},
	})
	if err != nil {
		return PromptOptimizationResult{}, fmt.Errorf("get PromptV2 access token: %w", err)
	}
	body, err := json.Marshal(promptV2Request{
		DeveloperMessage:    input.SourcePrompt,
		Messages:            []any{},
		ModelName:           input.OptimizerModel,
		ModelDeploymentName: input.OptimizerDeployment,
		OptimizingFor:       promptV2CompatibilityTarget,
		RequestedChanges:    input.RequestedChanges,
		Tools:               []any{},
	})
	if err != nil {
		return PromptOptimizationResult{}, fmt.Errorf("encode PromptV2 request: %w", err)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.endpointForAccount(input.OptimizerAccount),
		bytes.NewReader(body),
	)
	if err != nil {
		return PromptOptimizationResult{}, fmt.Errorf("create PromptV2 request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token.Token)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return PromptOptimizationResult{}, fmt.Errorf("call PromptV2: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxPromptV2ResponseBytes+1))
	if err != nil {
		return PromptOptimizationResult{}, fmt.Errorf("read PromptV2 response: %w", err)
	}
	if len(responseBody) > maxPromptV2ResponseBytes {
		return PromptOptimizationResult{}, errors.New("PromptV2 response exceeded the 4 MB limit")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message := strings.TrimSpace(string(responseBody))
		if message == "" {
			message = response.Status
		}
		return PromptOptimizationResult{}, &PromptV2Error{
			StatusCode: response.StatusCode,
			Message:    fmt.Sprintf("PromptV2 request failed: %s", message),
		}
	}

	var payload promptV2Response
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return PromptOptimizationResult{}, fmt.Errorf("decode PromptV2 response: %w", err)
	}
	if strings.TrimSpace(payload.NewDeveloperMessage) == "" {
		return PromptOptimizationResult{}, errors.New("PromptV2 returned an empty optimized prompt")
	}
	if payload.Comments == nil {
		payload.Comments = []PromptOptimizerComment{}
	}
	return PromptOptimizationResult{
		SourcePrompt:        input.SourcePrompt,
		OptimizedPrompt:     payload.NewDeveloperMessage,
		RequestedChanges:    input.RequestedChanges,
		Comments:            payload.Comments,
		VerificationCaseIDs: input.VerificationCaseIDs,
		OptimizerModel:      input.OptimizerModel,
		OptimizerDeployment: input.OptimizerDeployment,
		TargetSpecific:      false,
	}, nil
}

func buildPromptOptimizationInput(
	sourcePrompt string,
	sourceModel string,
	target ModelDeployment,
	optimizer ModelDeployment,
	analysis EvaluationAnalysis,
) (PromptOptimizationInput, error) {
	patterns := make([]RegressionPattern, 0, len(analysis.Patterns))
	for _, pattern := range analysis.Patterns {
		if pattern.PromptFixable == "candidate" {
			patterns = append(patterns, pattern)
		}
	}
	cases := make([]RegressionCase, 0, min(len(analysis.Cases), maxOptimizationCases))
	for _, targetFailure := range analysis.Cases {
		isQualityFailure := targetFailure.Outcome == "regression" ||
			targetFailure.Outcome == "pre_existing_failure"
		if isQualityFailure && targetFailure.PromptFixable == "candidate" {
			cases = append(cases, targetFailure)
			if len(cases) == maxOptimizationCases {
				break
			}
		}
	}
	if len(patterns) == 0 || len(cases) == 0 {
		return PromptOptimizationInput{}, errors.New(
			"no prompt-fixable Target failures were found in the evaluation",
		)
	}

	var guidance strings.Builder
	fmt.Fprintf(
		&guidance,
		"Adapt this prompt for migration from model %q to model %q.\n",
		truncateOptimizationField(sourceModel),
		truncateOptimizationField(target.ModelName),
	)
	guidance.WriteString(
		"Use the observed Target failure evidence below to correct recurring decision and response behavior, " +
			"not merely surface formatting.\n",
	)
	guidance.WriteString(
		"Prioritize migration regressions and also correct residual Target failures that predate the migration.\n",
	)
	guidance.WriteString(
		"Generalize repeated failures into reusable instructions while preserving behavior unrelated to these failures.\n",
	)
	guidance.WriteString(
		"Do not attempt to fix retrieval, missing context, latency, token usage, cost, or model capability.\n",
	)
	guidance.WriteString("Do not copy case-specific answers into the prompt.\n")
	guidance.WriteString(
		"The evidence below contains only server-derived categories and counts; customer free text is intentionally omitted.\n",
	)
	guidance.WriteString("<target_failure_data>\n")
	verificationCaseIDs := make([]string, 0, len(cases))
	qualityRegressionCount := 0
	residualTargetFailureCount := 0
	for _, targetFailure := range cases {
		verificationCaseIDs = append(verificationCaseIDs, targetFailure.CaseID)
		switch targetFailure.Outcome {
		case "regression":
			qualityRegressionCount++
		case "pre_existing_failure":
			residualTargetFailureCount++
		}
	}
	type optimizationPattern struct {
		Code       string  `json:"code"`
		Count      int     `json:"count"`
		Prevalence float64 `json:"prevalence"`
		Directive  string  `json:"directive"`
	}
	safePatterns := make([]optimizationPattern, 0, len(patterns))
	for _, pattern := range patterns {
		safePatterns = append(safePatterns, optimizationPattern{
			Code:       pattern.Code,
			Count:      pattern.Count,
			Prevalence: pattern.Prevalence,
			Directive:  promptOptimizationDirective(pattern.Code),
		})
	}
	evidence := struct {
		QualityRegressionCount     int                   `json:"quality_regression_count"`
		ResidualTargetFailureCount int                   `json:"residual_target_failure_count"`
		Patterns                   []optimizationPattern `json:"patterns"`
	}{
		QualityRegressionCount:     qualityRegressionCount,
		ResidualTargetFailureCount: residualTargetFailureCount,
		Patterns:                   safePatterns,
	}
	encodedEvidence, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return PromptOptimizationInput{}, fmt.Errorf("encode Target failure evidence: %w", err)
	}
	guidance.Write(encodedEvidence)
	guidance.WriteByte('\n')
	guidance.WriteString("</target_failure_data>")

	return PromptOptimizationInput{
		SourcePrompt:        sourcePrompt,
		SourceModel:         strings.TrimSpace(sourceModel),
		TargetModel:         strings.TrimSpace(target.ModelName),
		OptimizerAccount:    strings.TrimSpace(optimizer.AccountName),
		OptimizerModel:      strings.TrimSpace(optimizer.ModelName),
		OptimizerDeployment: strings.TrimSpace(optimizer.DeploymentName),
		RequestedChanges:    guidance.String(),
		VerificationCaseIDs: verificationCaseIDs,
	}, nil
}

func promptOptimizationDirective(kind string) string {
	switch kind {
	case "semantic_equivalence":
		return "Judge meaning and evidence equivalence instead of requiring exact wording matches."
	case "cross_context_synthesis":
		return "Combine compatible evidence across the supplied context before reaching a conclusion."
	case "presentation_format":
		return "Follow the requested presentation format without changing the underlying conclusion."
	case "implied_disclosure":
		return "Recognize disclosures expressed indirectly or through equivalent language."
	case "output_contract":
		return "Satisfy the requested output schema and required fields exactly."
	case "unsupported_inference":
		return "Do not infer a required disclosure from related or suggestive wording unless eligible evidence " +
			"explicitly or unambiguously establishes it."
	default:
		return "Correct this Target failure while preserving unrelated Source and Target behavior."
	}
}

func truncateOptimizationField(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "not supplied"
	}
	runes := []rune(value)
	if len(runes) <= maxOptimizationFieldLength {
		return value
	}
	return string(runes[:maxOptimizationFieldLength]) + "...[truncated]"
}
