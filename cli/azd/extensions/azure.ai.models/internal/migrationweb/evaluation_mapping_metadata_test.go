// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func canonicalMappingEnvelope(promptHash any) map[string]any {
	return map[string]any{
		"id": "suite-id",
		"suite": map[string]any{
			"prompt_sha256": promptHash,
		},
		"runs": map[string]any{
			"source": map[string]any{"model": "source-model", "run_id": "source-run"},
			"target": map[string]any{"model": "target-model", "run_id": "target-run"},
		},
		"cases": []any{map[string]any{
			"case_id": "a",
			"input":   map[string]any{"task": "Q"},
			"observations": map[string]any{
				"source": map[string]any{"output": "A"},
				"target": map[string]any{"output": "B"},
			},
			"assessments": []any{
				map[string]any{"subject": "source", "evaluator": "quality", "status": "pass"},
				map[string]any{"subject": "target", "evaluator": "quality", "status": "fail"},
			},
			"failure": map[string]any{"kind": "semantic_equivalence", "source": "customer"},
		}},
	}
}

func TestMappingCanonicalPromptBindingAcrossEndpoints(t *testing.T) {
	digest := sha256.Sum256([]byte("Source prompt"))
	expectedHash := fmt.Sprintf("%x", digest)
	for _, test := range []struct {
		name, adapter string
		hash          any
		valid         bool
	}{
		{"matching", "canonical", expectedHash, true},
		{"matching uppercase", "canonical", strings.ToUpper(expectedHash), true},
		{"mismatched", "canonical", strings.Repeat("0", 64), false},
		{"malformed", "canonical", "not-a-hash", false},
		{"wrong type", "canonical", false, false},
		{"generic switch cannot bypass", "generic", strings.Repeat("0", 64), false},
		{"generic matching", "generic", expectedHash, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := mappingTestFile(t, 0, "canonical.json", valueStringJSON(canonicalMappingEnvelope(test.hash)))
			files := []mappingFile{file}
			plan, _ := suggestMapping(files, "source-model", "target-model")
			require.Equal(t, "canonical", plan.Adapter)
			plan.Adapter = test.adapter
			fields := map[string]string{"sourceModelName": "source-model", "targetModelName": "target-model"}
			server := &Server{promptOptimizer: &recordingPromptOptimizer{}}
			response := httptest.NewRecorder()
			server.handleMappingRequest(response,
				mappingTestRequest(t, "/api/evaluation-mapping/preview", files, plan, fields))
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var preview MappingPreview
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &preview))
			require.Equal(t, test.valid, preview.Valid, "%+v", preview.Issues)
			require.Equal(t, "source-model", preview.SourceModel)
			require.Equal(t, "target-model", preview.TargetModel)
			require.Equal(t, "source-run", preview.SourceRunID)
			require.Equal(t, "target-run", preview.TargetRunID)
			fields["mappingConfirmed"] = "true"
			fields["evaluationSha256"] = preview.EvaluationSHA256
			fields["optimizerAccountName"] = "account"
			fields["optimizerModelName"] = "model"
			fields["optimizerDeploymentName"] = "deployment"
			if test.valid {
				optimizationPreview := previewMappedOptimization(t, server, files, plan, fields)
				fields["allowEvaluationContent"] = "true"
				fields["optimizationRequestSha256"] = optimizationPreview.RequestSHA256
			}
			for _, endpoint := range []struct {
				path string
				call http.HandlerFunc
			}{
				{"/api/evaluation-analysis", server.handleEvaluationAnalysis},
				{"/api/prompt-optimization", server.handlePromptOptimization},
			} {
				response := httptest.NewRecorder()
				endpoint.call(response, mappingTestRequest(t, endpoint.path, files, plan, fields))
				status := http.StatusOK
				if !test.valid {
					status = http.StatusBadRequest
				}
				require.Equal(t, status, response.Code, "%s: %s", endpoint.path, response.Body.String())
			}
		})
	}
}

func TestMappingCanonicalEnvelopeModelConflict(t *testing.T) {
	digest := sha256.Sum256([]byte("Source prompt"))
	file := mappingTestFile(t, 0, "canonical.json", valueStringJSON(canonicalMappingEnvelope(fmt.Sprintf("%x", digest))))
	for _, adapter := range []string{"canonical", "generic"} {
		t.Run(adapter, func(t *testing.T) {
			files := []mappingFile{file}
			plan, _ := suggestMapping(files, "", "")
			plan.Adapter = adapter
			preview, _ := previewMapping(files, plan, "wrong-model", "target-model")
			require.False(t, preview.Valid)
			require.Contains(t, valueStringJSON(preview.Issues), "MODEL_IDENTITY_CONFLICT")
		})
	}
}

func TestMappingPromptHashChecksAllJSONLRecords(t *testing.T) {
	digest := sha256.Sum256([]byte("Source prompt"))
	correct := fmt.Sprintf("%x", digest)
	content := `{"id":"a","question":"Q","old":"A","new":"B","oldOK":true,"newOK":false,` +
		`"suite":{"prompt_sha256":"` + correct + `"}}` + "\n" +
		`{"id":"b","question":"Q","old":"A","new":"B","oldOK":true,"newOK":false,` +
		`"suite":{"prompt_sha256":"` + strings.Repeat("0", 64) + `"}}`
	files := []mappingFile{mappingTestFile(t, 0, "data.jsonl", content)}
	response := httptest.NewRecorder()
	(&Server{}).handleMappingRequest(response,
		mappingTestRequest(t, "/api/evaluation-mapping/preview", files, mappingTestPlan(), nil))
	var preview MappingPreview
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &preview))
	require.False(t, preview.Valid)
	require.Contains(t, valueStringJSON(preview.Issues), "PROMPT_HASH_MISMATCH")
}
