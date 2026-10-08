// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMappedRunWindowsPreserveMonitorScope(t *testing.T) {
	for _, start := range []string{"2026-09-17T08:00:00+08:00", "invalid"} {
		t.Run(start, func(t *testing.T) {
			content := fmt.Sprintf(`{
				"runs":{
					"source":{"started_at":%q,"completed_at":"2026-09-17T01:30:00Z"},
					"target":{"started_at":"2026-09-17T01:00:00Z","completed_at":"2026-09-17T02:00:00Z"}},
				"cases":[{"id":"a","question":"Q","old":"A","new":"B","oldOK":true,"newOK":false}]
			}`, start)
			files := []mappingFile{mappingTestFile(t, 0, "combined.json", content)}
			plan := mappingTestPlan()
			plan.Source.Collection, plan.Target.Collection = "/cases", "/cases"
			preview, _ := previewMapping(files, plan, "", "")
			require.True(t, preview.Valid, "%+v", preview.Issues)
			if start == "invalid" {
				require.Empty(t, preview.StartedAt)
				require.Empty(t, preview.CompletedAt)
				require.True(t, slices.ContainsFunc(preview.Issues, func(issue MappingIssue) bool {
					return issue.Code == "INVALID_RUN_TIME"
				}))
			} else {
				require.Equal(t, "2026-09-17T00:00:00Z", preview.StartedAt)
				require.Equal(t, "2026-09-17T02:00:00Z", preview.CompletedAt)
			}
		})
	}
}

func TestCanonicalMappingHTTPPreviewRunWindow(t *testing.T) {
	digest := sha256.Sum256([]byte("Source prompt"))
	for _, test := range []struct {
		name, sourceStart, sourceEnd string
		wantStart, wantEnd           string
	}{
		{
			name:        "UTC aggregation across offsets",
			sourceStart: "2026-09-17T08:00:00+08:00",
			sourceEnd:   "2026-09-17T01:30:00Z",
			wantStart:   "2026-09-17T00:00:00Z",
			wantEnd:     "2026-09-17T02:00:00Z",
		},
		{
			name:        "fractional seconds",
			sourceStart: "2026-09-17T08:00:00.123456789+08:00",
			sourceEnd:   "2026-09-17T03:00:00.5Z",
			wantStart:   "2026-09-17T00:00:00.123456789Z",
			wantEnd:     "2026-09-17T03:00:00.5Z",
		},
		{"invalid date", "2026-02-30T00:00:00Z", "2026-09-17T01:30:00Z", "", ""},
		{"incomplete window", "2026-09-17T00:00:00Z", "", "", ""},
		{"reversed window", "2026-09-17T03:00:00Z", "2026-09-17T01:30:00Z", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			envelope := canonicalMappingEnvelope(fmt.Sprintf("%x", digest))
			envelope["runs"] = map[string]any{
				"source": map[string]any{
					"model": "source-model", "run_id": "source-run",
					"started_at": test.sourceStart, "completed_at": test.sourceEnd,
				},
				"target": map[string]any{
					"model": "target-model", "run_id": "target-run",
					"started_at": "2026-09-17T01:00:00Z", "completed_at": "2026-09-17T02:00:00Z",
				},
			}
			files := []mappingFile{mappingTestFile(t, 0, "canonical.json", valueStringJSON(envelope))}
			plan, _ := suggestMapping(files, "source-model", "target-model")
			require.Equal(t, "canonical", plan.Adapter)
			response := httptest.NewRecorder()
			(&Server{}).handleMappingRequest(response,
				mappingTestRequest(t, "/api/evaluation-mapping/preview", files, plan, map[string]string{
					"sourceModelName": "source-model", "targetModelName": "target-model",
				}))
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var preview MappingPreview
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &preview))
			require.True(t, preview.Valid, "%+v", preview.Issues)
			require.Equal(t, test.wantStart, preview.StartedAt)
			require.Equal(t, test.wantEnd, preview.CompletedAt)
			require.Equal(t, "source-model", preview.SourceModel)
			require.Equal(t, "target-model", preview.TargetModel)
			require.Equal(t, "source-run", preview.SourceRunID)
			require.Equal(t, "target-run", preview.TargetRunID)
			if test.wantStart == "" {
				var payload map[string]any
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
				require.NotContains(t, payload, "startedAt")
				require.NotContains(t, payload, "completedAt")
				require.True(t, slices.ContainsFunc(preview.Issues, func(issue MappingIssue) bool {
					return issue.Code == "INVALID_RUN_TIME"
				}))
			}
		})
	}
}
