// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFoundryStructuredExpectedOutputConsistency(t *testing.T) {
	for _, scenario := range []string{"matching", "source-conflict", "target-conflict", "source-missing"} {
		t.Run(scenario, func(t *testing.T) {
			expected := map[string]any{"answer": true}
			datasetRow := map[string]any{
				"id": 1, "query": "Q", "description": "Structured reference", "candidate_response": expected,
			}
			source := foundryRunRecord(datasetRow, "Source", "gpt-4.1-mini", "source-run", true)
			target := foundryRunRecord(datasetRow, "Target", "gpt-5.4", "target-run", false)
			sourceItem, ok := source["datasource_item"].(map[string]any)
			require.True(t, ok)
			targetItem, ok := target["datasource_item"].(map[string]any)
			require.True(t, ok)
			switch scenario {
			case "source-conflict":
				sourceItem["candidate_response"] = map[string]any{"answer": false}
			case "target-conflict":
				targetItem["candidate_response"] = map[string]any{"answer": false}
			case "source-missing":
				delete(sourceItem, "candidate_response")
			}
			datasetBytes := marshalJSONLines(t, []map[string]any{datasetRow})
			sourceBytes := marshalJSONLines(t, []map[string]any{source})
			targetBytes := marshalJSONLines(t, []map[string]any{target})
			legacy, err := analyzeFoundryEvaluationBundle(
				"dataset.jsonl", datasetBytes, "source.jsonl", sourceBytes, "target.jsonl", targetBytes,
			)
			files := []mappingFile{
				mappingTestFile(t, 0, "dataset.jsonl", string(datasetBytes)),
				mappingTestFile(t, 1, "source.jsonl", string(sourceBytes)),
				mappingTestFile(t, 2, "target.jsonl", string(targetBytes)),
			}
			plan, _ := suggestMapping(files, "gpt-4.1-mini", "gpt-5.4")
			require.Equal(t, "foundry", plan.Adapter)
			preview, analysis := previewMapping(files, plan, "gpt-4.1-mini", "gpt-5.4")
			if scenario == "matching" {
				require.NoError(t, err)
				require.True(t, preview.Valid, "%+v", preview.Issues)
				require.Equal(t, expected, legacy.Cases[0].ExpectedOutput)
				require.Equal(t, expected, analysis.Cases[0].ExpectedOutput)
			} else {
				require.ErrorContains(t, err, "mismatched candidate_response")
				require.False(t, preview.Valid)
			}
		})
	}
}
