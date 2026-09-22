// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type validationRow struct {
	ID, Role, Status, Model, Input string
	Score                          *float64
}

func validationDocument(t *testing.T, promptHash string, rows ...validationRow) []byte {
	t.Helper()
	values := make([]map[string]any, len(rows))
	for index, row := range rows {
		values[index] = map[string]any{
			"id": row.ID, "role": row.Role, "input": row.Input, "output": "output",
			"status": row.Status, "model": row.Model,
		}
		if row.Score != nil {
			values[index]["score"] = *row.Score
		}
	}
	document := map[string]any{"rows": values}
	if promptHash != "" {
		document["suite"] = map[string]any{"prompt_sha256": promptHash}
	}
	content, err := json.Marshal(document)
	require.NoError(t, err)
	return content
}

func validationPlan() EvaluationMapping {
	lane := func(role string) MappingLane {
		return MappingLane{
			File: 0, Collection: "/rows", Filters: []MappingFilter{{Path: "/role", Value: role}},
			CaseID: "/id", Input: "/input", Output: "/output", Model: "/model",
			Evaluator: MappingEvaluator{Status: "/status", Filters: []MappingFilter{}},
			PassRule:  "reported", Operator: "gte",
		}
	}
	return EvaluationMapping{
		Version: 1, Adapter: "generic", Source: lane("source"), Target: lane("target"),
	}
}

func validationBaselineRows(targets ...validationRow) []validationRow {
	rows := make([]validationRow, 0, len(targets)*2)
	for _, target := range targets {
		var sourceScore *float64
		if target.Score != nil {
			sourceScore = new(1.0)
		}
		rows = append(rows,
			validationRow{
				ID: target.ID, Role: "source", Status: "pass", Model: "source-model",
				Input: target.Input, Score: sourceScore,
			},
			target,
		)
	}
	return rows
}

func validationDigest(files []mappingFile, plan EvaluationMapping, prompt, sourceModel, targetModel string) string {
	promptHash := sha256.Sum256([]byte(prompt))
	bound := sha256.Sum256([]byte(fmt.Sprintf(
		"%s:%x", mappingDigest(files, plan, sourceModel, targetModel), promptHash)))
	return fmt.Sprintf("%x", bound)
}

func validationRequest(
	t *testing.T, baseline []byte, adapted []byte, plan EvaluationMapping, candidate, evaluationHash string,
) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writeMultipartFile(t, writer, "files", "baseline.json", baseline)
	writeMultipartFile(t, writer, "prompt", "prompt.txt", []byte("original prompt"))
	writeMultipartFile(t, writer, "adaptedTarget", "adapted.json", adapted)
	for name, value := range map[string]string{
		"mapping": valueStringJSON(plan), "mappingConfirmed": "true",
		"evaluationSha256": evaluationHash, "sourceModelName": "source-model",
		"targetModelName": "target-model", "candidatePrompt": candidate,
	} {
		require.NoError(t, writer.WriteField(name, value))
	}
	require.NoError(t, writer.Close())
	request := httptestRequest(t, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func httptestRequest(t *testing.T, body *bytes.Buffer) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/api/evaluation-validation", body)
	require.NoError(t, err)
	return request
}

func runValidation(
	t *testing.T, baselineRows, adaptedRows []validationRow, candidateHash string, mutate func(*EvaluationMapping),
) (int, EvaluationValidation, string) {
	t.Helper()
	promptHash := sha256.Sum256([]byte("original prompt"))
	baseline := validationDocument(t, fmt.Sprintf("%x", promptHash), baselineRows...)
	adapted := validationDocument(t, candidateHash, adaptedRows...)
	plan := validationPlan()
	if mutate != nil {
		mutate(&plan)
	}
	files := []mappingFile{mappingTestFile(t, 0, "baseline.json", string(baseline))}
	digest := validationDigest(files, plan, "original prompt", "source-model", "target-model")
	response := httptest.NewRecorder()
	(&Server{}).handleEvaluationValidation(
		response, validationRequest(t, baseline, adapted, plan, "candidate prompt", digest))
	var result EvaluationValidation
	if response.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	}
	return response.Code, result, response.Body.String()
}

func TestEvaluationValidationTransitions(t *testing.T) {
	baseline := validationBaselineRows(
		validationRow{ID: "resolved", Role: "target", Status: "fail", Model: "target-model", Input: "a"},
		validationRow{ID: "remaining", Role: "target", Status: "fail", Model: "target-model", Input: "b"},
		validationRow{ID: "new", Role: "target", Status: "pass", Model: "target-model", Input: "c"},
		validationRow{ID: "preserved", Role: "target", Status: "pass", Model: "target-model", Input: "d"},
	)
	adapted := []validationRow{
		{ID: "resolved", Role: "target", Status: "pass", Model: "target-model", Input: "a"},
		{ID: "remaining", Role: "target", Status: "fail", Model: "target-model", Input: "b"},
		{ID: "new", Role: "target", Status: "fail", Model: "target-model", Input: "c"},
		{ID: "preserved", Role: "target", Status: "pass", Model: "target-model", Input: "d"},
	}
	candidateHash := sha256.Sum256([]byte("candidate prompt"))
	status, result, body := runValidation(t, baseline, adapted, fmt.Sprintf("%x", candidateHash), nil)
	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, 4, result.CaseCount)
	require.Equal(t, 1, result.ResolvedCount)
	require.Equal(t, 1, result.RemainingCount)
	require.Equal(t, 1, result.NewFailureCount)
	require.Equal(t, 1, result.PreservedCount)
	require.Equal(t, []string{"resolved"}, result.ResolvedCaseIDs)
	require.Equal(t, []string{"remaining"}, result.RemainingCaseIDs)
	require.Equal(t, []string{"new"}, result.NewFailureCaseIDs)
	require.Empty(t, result.UnclassifiedCaseIDs)
	require.NotNil(t, result.Issues)
	require.NotNil(t, result.Warnings)
	require.False(t, result.ReadyForRolloutReview)
}

func TestEvaluationValidationReadyForRolloutReview(t *testing.T) {
	baseline := validationBaselineRows(
		validationRow{ID: "a", Role: "target", Status: "fail", Model: "target-model", Input: "a"})
	adapted := []validationRow{
		{ID: "a", Role: "target", Status: "pass", Model: "target-model", Input: "a"},
	}
	candidateHash := sha256.Sum256([]byte("candidate prompt"))
	status, result, body := runValidation(t, baseline, adapted, fmt.Sprintf("%x", candidateHash), nil)
	require.Equal(t, http.StatusOK, status, body)
	require.True(t, result.ReadyForRolloutReview)
	require.Empty(t, result.Issues)
	require.Empty(t, result.Warnings)
}

func TestEvaluationValidationMissingExtraUnknownAndDuplicate(t *testing.T) {
	base := validationBaselineRows(
		validationRow{ID: "a", Role: "target", Status: "fail", Model: "target-model", Input: "a"},
		validationRow{ID: "missing", Role: "target", Status: "pass", Model: "target-model", Input: "m"},
	)
	t.Run("missing and extra", func(t *testing.T) {
		status, result, body := runValidation(t, base, []validationRow{
			{ID: "a", Role: "target", Status: "pass", Model: "target-model", Input: "a"},
			{ID: "extra", Role: "target", Status: "pass", Model: "target-model", Input: "e"},
		}, "", nil)
		require.Equal(t, http.StatusOK, status, body)
		require.Equal(t, 1, result.MissingCount)
		require.Equal(t, 1, result.ExtraCount)
		require.False(t, result.ReadyForRolloutReview)
	})
	for _, statusValue := range []string{"unknown", "error", "skipped"} {
		t.Run(statusValue, func(t *testing.T) {
			status, result, body := runValidation(t, base, []validationRow{
				{ID: "a", Role: "target", Status: statusValue, Model: "target-model", Input: "a"},
				{ID: "missing", Role: "target", Status: "pass", Model: "target-model", Input: "m"},
			}, "", nil)
			require.Equal(t, http.StatusOK, status, body)
			require.Equal(t, 1, result.UnclassifiedCount)
			require.Equal(t, []string{"a"}, result.UnclassifiedCaseIDs)
		})
	}
	t.Run("duplicate", func(t *testing.T) {
		status, result, body := runValidation(t, base, []validationRow{
			{ID: "a", Role: "target", Status: "pass", Model: "target-model", Input: "a"},
			{ID: "a", Role: "target", Status: "pass", Model: "target-model", Input: "a"},
			{ID: "missing", Role: "target", Status: "pass", Model: "target-model", Input: "m"},
		}, "", nil)
		require.Equal(t, http.StatusOK, status, body)
		require.Contains(t, result.Issues, MappingIssue{
			Severity: "error", Code: "DUPLICATE_CASE_KEY",
			Message: "Target has a duplicate case ID", CaseID: "a",
		})
		require.False(t, result.ReadyForRolloutReview)
	})
}

func TestEvaluationValidationRequestGuards(t *testing.T) {
	rows := validationBaselineRows(
		validationRow{ID: "a", Role: "target", Status: "fail", Model: "target-model", Input: "a"})
	promptHash := sha256.Sum256([]byte("original prompt"))
	baseline := validationDocument(t, fmt.Sprintf("%x", promptHash), rows...)
	adapted := validationDocument(t, "", validationRow{
		ID: "a", Role: "target", Status: "pass", Model: "target-model", Input: "a"})
	plan := validationPlan()
	files := []mappingFile{mappingTestFile(t, 0, "baseline.json", string(baseline))}
	digest := validationDigest(files, plan, "original prompt", "source-model", "target-model")
	for _, test := range []struct {
		name, candidate, hash string
	}{
		{"stale baseline", "candidate", strings.Repeat("0", 64)},
		{"empty candidate", " \n", digest},
		{"large candidate", strings.Repeat("x", maxPromptBytes+1), digest},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			(&Server{}).handleEvaluationValidation(
				response, validationRequest(t, baseline, adapted, plan, test.candidate, test.hash))
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		})
	}
}

func TestEvaluationValidationReusesThresholdAndChecksMetadata(t *testing.T) {
	failing, passing := 0.49, 0.5
	baseline := validationBaselineRows(
		validationRow{
			ID: "a", Role: "target", Model: "target-model", Input: "a", Score: &failing,
		},
	)
	adapted := []validationRow{{
		ID: "a", Role: "target", Model: "target-model", Input: "a", Score: &passing,
	}}
	threshold := func(plan *EvaluationMapping) {
		for _, lane := range []*MappingLane{&plan.Source, &plan.Target} {
			lane.PassRule = "threshold"
			lane.Evaluator.Status = ""
			lane.Evaluator.Score = "/score"
			lane.Threshold = new(0.5)
		}
	}
	status, result, body := runValidation(t, baseline, adapted, strings.Repeat("0", 64), threshold)
	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, 1, result.ResolvedCount)
	require.Contains(t, result.Warnings, candidatePromptProvenanceWarning)
	require.Contains(t, result.Issues, MappingIssue{
		Severity: "error", Code: "PROMPT_HASH_MISMATCH",
		Message: "suite.prompt_sha256 does not match the candidate prompt",
	})

	conflict := []validationRow{{
		ID: "a", Role: "target", Status: "pass", Model: "other-model", Input: "a",
	}}
	status, result, body = runValidation(t, validationBaselineRows(validationRow{
		ID: "a", Role: "target", Status: "fail", Model: "target-model", Input: "a",
	}), conflict, "", nil)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, result.Issues, MappingIssue{
		Severity: "error", Code: "MODEL_IDENTITY_CONFLICT",
		Message: "Target reported model conflicts with binding", CaseID: "a",
	})
}

func TestEvaluationValidationSeparatedCSVTargetUsesAdaptedFileZero(t *testing.T) {
	source := []byte("id,input,output,model,score\na,Q,A,source-model,1\n")
	target := []byte("id,input,output,model,score\na,Q,B,target-model,0.4\n")
	adapted := []byte("id,input,output,model,score\na,Q,C,target-model,0.5\n")
	plan := EvaluationMapping{
		Version: 1,
		Adapter: "generic",
		Source: MappingLane{
			File: 0, CaseID: "/id", Input: "/input", Output: "/output", Model: "/model",
			Evaluator: MappingEvaluator{Score: "/score", Filters: []MappingFilter{}},
			PassRule:  "threshold", Operator: "gte", Threshold: new(0.5), Filters: []MappingFilter{},
		},
		Target: MappingLane{
			File: 1, CaseID: "/id", Input: "/input", Output: "/output", Model: "/model",
			Evaluator: MappingEvaluator{Score: "/score", Filters: []MappingFilter{}},
			PassRule:  "threshold", Operator: "gte", Threshold: new(0.5), Filters: []MappingFilter{},
		},
	}
	files := []mappingFile{
		mappingTestFile(t, 0, "source.csv", string(source)),
		mappingTestFile(t, 1, "target.csv", string(target)),
	}
	digest := validationDigest(files, plan, "original prompt", "source-model", "target-model")
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writeMultipartFile(t, writer, "files", "source.csv", source)
	writeMultipartFile(t, writer, "files", "target.csv", target)
	writeMultipartFile(t, writer, "prompt", "prompt.txt", []byte("original prompt"))
	writeMultipartFile(t, writer, "adaptedTarget", "adapted.csv", adapted)
	for name, value := range map[string]string{
		"mapping": valueStringJSON(plan), "mappingConfirmed": "true",
		"evaluationSha256": digest, "sourceModelName": "source-model",
		"targetModelName": "target-model", "candidatePrompt": "candidate prompt",
	} {
		require.NoError(t, writer.WriteField(name, value))
	}
	require.NoError(t, writer.Close())
	request := httptestRequest(t, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	(&Server{}).handleEvaluationValidation(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result EvaluationValidation
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Equal(t, digest, result.BaselineEvaluationSHA256)
	require.Equal(t, 1, result.ResolvedCount)
	require.Equal(t, []string{"a"}, result.ResolvedCaseIDs)
}

func TestEvaluationValidationEndpointRequiresAuthentication(t *testing.T) {
	server, err := NewServer(ServerOptions{Provider: staticProvider{}})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx)
	}()
	response, err := http.Post(server.URL()+"/api/evaluation-validation", "multipart/form-data", nil)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)

	browserURL, err := url.Parse(server.BrowserURL())
	require.NoError(t, err)
	request, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, server.URL()+"/api/evaluation-validation", nil)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+browserURL.Query().Get("token"))
	request.Header.Set("Content-Type", "multipart/form-data")
	response, err = http.DefaultClient.Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	cancel()
	require.NoError(t, <-done)
}
