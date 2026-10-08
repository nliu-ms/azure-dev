// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMappingProposerOnlySendsStructure(t *testing.T) {
	files := []mappingFile{mappingTestFile(t, 0, "PRIVATE_FILENAME.json", `[
		{"id":"PRIVATE_ID","question":"PRIVATE_INPUT","old":"PRIVATE_SOURCE","new":"PRIVATE_TARGET",
		 "oldOK":true,"newOK":false,"reason":"PRIVATE_REASON"}]`)}
	files[0].profile.Collections[0].Path = "/PRIVATE_COLLECTION_NAME"
	plan := mappingTestProposal()
	proposalJSON := strings.TrimSuffix(valueStringJSON(plan), "}") + `,"runId":""}`
	var received string
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		require.Equal(t, "/openai/v1/chat/completions", r.URL.Path)
		content, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		received = string(content)
		require.NotContains(t, received, "PRIVATE_")
		require.NotContains(t, received, files[0].profile.SHA256)
		require.NotContains(t, received, `"format"`)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(content, &payload))
		require.Equal(t, "deployment", payload["model"])
		messages, ok := payload["messages"].([]any)
		require.True(t, ok)
		require.Len(t, messages, 2)
		userMessage, ok := messages[1].(map[string]any)
		require.True(t, ok)
		userContent, ok := userMessage["content"].(string)
		require.True(t, ok)
		require.Contains(t, userContent, `"rowCount":1`)
		require.Contains(t, userContent, `"distinct":1`)
		responseFormat, ok := payload["response_format"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "json_schema", responseFormat["type"])
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"finish_reason": "stop",
				"message":       map[string]string{"content": proposalJSON},
			}},
		}))
	}))
	defer endpoint.Close()
	proposer, err := NewMappingProposer(staticTokenCredential{})
	require.NoError(t, err)
	client, ok := proposer.(*azureMappingProposer)
	require.True(t, ok)
	require.Equal(t, "https://account.openai.azure.com/openai/v1/chat/completions", client.endpointForAccount("account"))
	client.endpointForAccount = func(string) string { return endpoint.URL + "/openai/v1/chat/completions" }
	result, err := client.Propose(t.Context(), MappingProposalInput{
		Files: mappingStructuralProfile(files), AccountName: "account", ModelName: "model", DeploymentName: "deployment",
	})
	require.NoError(t, err)
	require.Equal(t, plan, result)
	require.Contains(t, received, "question")
}

func TestMappingProposerFailures(t *testing.T) {
	files := []mappingFile{mappingTestFile(t, 0, "data.json", `[
		{"id":"a","question":"Q","old":"A","new":"B","oldOK":true,"newOK":false}]`)}
	base := mappingTestProposal()
	for _, test := range []struct {
		name, content string
		status        int
		delay         bool
	}{
		{"invalid JSON", `{`, 200, false},
		{"no choices", `{}`, 200, false},
		{"refused", `{"choices":[{"message":{"refusal":"no"}}]}`, 200, false},
		{"truncated", `{"choices":[{"finish_reason":"length","message":{"content":"{}"}}]}`, 200, false},
		{"wrong schema", `{"choices":[{"message":{"content":"{\"version\":2}"}}]}`, 200, false},
		{"unknown JSON keys", `{"choices":[{"message":{"content":"{\"code\":\"execute\"}"}}]}`, 200, false},
		{"HTTP failure", `{"error":"PRIVATE_SERVER_BODY"}`, 403, false},
		{"oversize", strings.Repeat("x", 64*1024+1), 200, false},
		{"timeout", "", 200, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			release := make(chan struct{})
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.delay {
					select {
					case <-r.Context().Done():
					case <-release:
					}
					return
				}
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.content)
			}))
			defer endpoint.Close()
			defer close(release)
			proposer, err := NewMappingProposer(staticTokenCredential{})
			require.NoError(t, err)
			client, ok := proposer.(*azureMappingProposer)
			require.True(t, ok)
			client.endpointForAccount = func(string) string { return endpoint.URL }
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			_, err = client.Propose(ctx, MappingProposalInput{
				Files:       mappingStructuralProfile(files),
				AccountName: "account", ModelName: "model", DeploymentName: "deployment",
			})
			require.Error(t, err)
			require.NotContains(t, err.Error(), "PRIVATE_SERVER_BODY")
		})
	}
	for _, test := range []struct {
		name string
		edit func(*EvaluationMapping)
	}{
		{"invented path", func(plan *EvaluationMapping) { plan.Target.Output = "/invented" }},
		{"invented threshold", func(plan *EvaluationMapping) { plan.Target.Threshold = new(0.5) }},
		{"invented value", func(plan *EvaluationMapping) {
			plan.Target.Filters = []MappingFilter{{Path: "/id", Value: "guessed"}}
		}},
		{"operational metric", func(plan *EvaluationMapping) { plan.Target.LatencyMs = "/latency" }},
		{"unknown file", func(plan *EvaluationMapping) { plan.Target.File = 3 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := base
			test.edit(&plan)
			require.Error(t, validateMappingProposal(plan, mappingStructuralProfile(files)))
		})
	}
}

type recordingMappingProposer struct {
	calls int
	input MappingProposalInput
	err   error
}

func (p *recordingMappingProposer) Propose(_ context.Context, input MappingProposalInput) (EvaluationMapping, error) {
	p.calls++
	p.input = input
	return mappingTestProposal(), p.err
}

func TestMappingProposalHTTPAuthenticationAndManualFallback(t *testing.T) {
	files := []mappingFile{mappingTestFile(t, 0, "PRIVATE_FILENAME.json", `[
		{"id":"PRIVATE_CASE","question":"PRIVATE_INPUT","old":"A","new":"B","oldOK":true,"newOK":false}]`)}
	proposer := &recordingMappingProposer{}
	server := &Server{mappingProposer: proposer, token: "secret"}
	fields := map[string]string{
		"optimizerAccountName": "account", "optimizerModelName": "model", "optimizerDeploymentName": "deployment",
	}
	request := mappingTestRequest(t, "/api/evaluation-mapping/propose", files, mappingTestPlan(), fields)
	response := httptest.NewRecorder()
	server.authorize(server.handleMappingRequest)(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Zero(t, proposer.calls)
	request = mappingTestRequest(t, "/api/evaluation-mapping/propose", files, mappingTestPlan(), fields)
	response = httptest.NewRecorder()
	server.handleMappingRequest(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, 1, proposer.calls)
	require.NotContains(t, valueStringJSON(proposer.input), "PRIVATE_")
	var profile MappingProfileResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &profile))
	require.Empty(t, profile.Mapping.Source.Collection)
	require.Empty(t, profile.Mapping.Target.Collection)
	proposer.err = errors.New("unavailable")
	response = httptest.NewRecorder()
	server.handleMappingRequest(response,
		mappingTestRequest(t, "/api/evaluation-mapping/propose", files, mappingTestPlan(), fields))
	require.Equal(t, http.StatusBadGateway, response.Code)
	response = httptest.NewRecorder()
	server.handleMappingRequest(response,
		mappingTestRequest(t, "/api/evaluation-mapping/preview", files, mappingTestPlan(), nil))
	require.Equal(t, http.StatusOK, response.Code)
	var preview MappingPreview
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &preview))
	require.True(t, preview.Valid)
	require.Equal(t, 2, proposer.calls)
}

func TestMappingProposerRejectsEndpointInjection(t *testing.T) {
	proposer, err := NewMappingProposer(staticTokenCredential{})
	require.NoError(t, err)
	for _, name := range []string{"https://localhost", "account.evil.example", "account/path", "account@evil"} {
		_, err := proposer.Propose(t.Context(), MappingProposalInput{
			AccountName: name, ModelName: "model", DeploymentName: "deployment",
		})
		require.Error(t, err)
	}
}

func mappingTestProposal() EvaluationMapping {
	plan := mappingTestPlan()
	plan.Source.Collection, plan.Target.Collection = "collection:0", "collection:0"
	return plan
}
