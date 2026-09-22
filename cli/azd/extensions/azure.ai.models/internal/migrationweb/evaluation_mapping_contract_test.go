// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func requireMappingJSONArrays(t *testing.T, value any) {
	t.Helper()
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if slices.Contains([]string{
				"files", "collections", "fields", "types", "filters", "issues", "cases", "warnings",
			}, key) {
				_, ok := child.([]any)
				require.True(t, ok, "%s must serialize as an array, got %T", key, child)
			}
			requireMappingJSONArrays(t, child)
		}
	case []any:
		for _, child := range value {
			requireMappingJSONArrays(t, child)
		}
	}
}

func requireSerializedMappingArrays(t *testing.T, value any) {
	t.Helper()
	content, err := json.Marshal(value)
	require.NoError(t, err)
	var decoded any
	require.NoError(t, json.Unmarshal(content, &decoded))
	requireMappingJSONArrays(t, decoded)
}

func TestMappingResponseArraysAreNeverNull(t *testing.T) {
	for _, response := range []MappingProfileResponse{
		{},
		{Files: []MappingFileProfile{
			{},
			{Collections: []MappingCollection{
				{},
				{Fields: []MappingField{{Path: "/field"}}},
			}},
		}},
	} {
		normalizeMappingProfileResponse(&response)
		requireSerializedMappingArrays(t, response)
	}
	profile := mappingStructuralProfile([]mappingFile{
		{},
		{profile: MappingFileProfile{Collections: []MappingCollection{
			{},
			{Fields: []MappingField{{Path: "/field"}}},
		}}},
	})
	requireSerializedMappingArrays(t, struct {
		Files []MappingSchemaFile `json:"files"`
	}{profile})
}

func TestMappingPreviewConfirmationInvariants(t *testing.T) {
	for _, test := range []struct {
		name    string
		preview MappingPreview
		valid   bool
	}{
		{"comparable", MappingPreview{Valid: true, ComparableCount: 1}, true},
		{"zero comparable", MappingPreview{Valid: true}, false},
		{"source only", MappingPreview{Valid: true, ComparableCount: 1, SourceOnly: 1}, false},
		{"target only", MappingPreview{Valid: true, ComparableCount: 1, TargetOnly: 1}, false},
		{"hard issue", MappingPreview{
			Valid: true, ComparableCount: 1, Issues: []MappingIssue{{Severity: "error"}},
		}, false},
		{"warning", MappingPreview{
			Valid: true, ComparableCount: 1, Issues: []MappingIssue{{Severity: "warning"}},
		}, true},
		{"omitted hard issue remains invalid", MappingPreview{Valid: false, ComparableCount: 1}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			normalizeMappingPreview(&test.preview)
			require.Equal(t, test.valid, test.preview.Valid)
			requireSerializedMappingArrays(t, test.preview)
		})
	}
}

func TestMappingHTTPArraysForEmptyAndPopulatedCollections(t *testing.T) {
	for _, test := range []struct {
		name, content string
		propose       bool
	}{
		{"empty array", `[]`, false},
		{"empty object", `[{}]`, false},
		{"populated", `[{"id":"a","question":"Q","old":"A","new":"B","oldOK":true,"newOK":false}]`, true},
	} {
		files := []mappingFile{mappingTestFile(t, 0, "data.json", test.content)}
		actions := []string{"profile", "preview"}
		if test.propose {
			actions = append(actions, "propose")
		}
		for _, action := range actions {
			t.Run(action+"/"+test.name, func(t *testing.T) {
				server := &Server{mappingProposer: &recordingMappingProposer{}}
				fields := map[string]string{
					"optimizerAccountName": "account", "optimizerModelName": "model",
					"optimizerDeploymentName": "deployment",
				}
				response := httptest.NewRecorder()
				server.handleMappingRequest(response,
					mappingTestRequest(t, "/api/evaluation-mapping/"+action, files, mappingTestPlan(), fields))
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				var decoded any
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &decoded))
				requireMappingJSONArrays(t, decoded)
			})
		}
	}
}

func TestEvaluationAnalysisIncludesEmptyWarningsArray(t *testing.T) {
	analysis, err := buildEvaluationAnalysis("data.json", "json", "", []string{}, nil)
	require.NoError(t, err)
	content, err := json.Marshal(analysis)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(content, &decoded))
	require.Equal(t, []any{}, decoded["warnings"])
}
