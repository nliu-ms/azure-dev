// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeMappingKeepsSelectedQuality(t *testing.T) {
	content := `[
		{"case_id":"a","input":{"task":"Q"},"observations":{"source":{"output":"A"},"target":{"output":"B"}},
		 "assessments":[
		  {"evaluator":"quality","subject":"source","status":"unknown"},
		  {"evaluator":"quality","subject":"target","status":"unknown"},
		  {"evaluator":"recall","subject":"source","status":"pass"},
		  {"evaluator":"recall","subject":"target","status":"pass"}]},
		{"case_id":"b","input":{"task":"R"},"observations":{"source":{"output":"C"},"target":{"output":"D"}},
		 "assessments":[
		  {"evaluator":"quality","subject":"source","status":"pass"},
		  {"evaluator":"quality","subject":"target","status":"fail"},
		  {"evaluator":"recall","subject":"source","status":"pass"},
		  {"evaluator":"recall","subject":"target","status":"pass"}]}]`
	files := []mappingFile{mappingTestFile(t, 0, "paired.json", content)}
	plan, _ := suggestMapping(files, "", "")
	require.Equal(t, "canonical", plan.Adapter)
	require.Contains(t, plan.Source.Evaluator.Filters, MappingFilter{Path: "/evaluator", Value: "quality"})
	for _, adapter := range []string{"canonical", "generic"} {
		t.Run(adapter, func(t *testing.T) {
			plan.Adapter = adapter
			preview, analysis := previewMapping(files, plan, "", "")
			require.True(t, preview.Valid, "%+v", preview.Issues)
			require.Equal(t, 1, preview.ComparableCount)
			require.Equal(t, 1, preview.UnclassifiedCount)
			require.Equal(t, 1, analysis.Regressions)
			require.Zero(t, analysis.Stable)
			require.Equal(t, "unknown", preview.Cases[0].SourceStatus)
		})
	}
	t.Run("Meera does not substitute recall", func(t *testing.T) {
		item := evaluationCase{assessments: []evaluationAssessment{
			{evaluator: "recall", subject: "source", status: "pass"},
			{evaluator: "recall", subject: "target", status: "pass"},
		}}
		preview := MappingPreview{}
		selected := nativeSelectedAssessment(item, MappingLane{}, "meera", "source", &preview)
		require.Equal(t, "unknown", selected.status)
	})
}

func TestNativeFoundryMissingQualityDoesNotUseFluency(t *testing.T) {
	dataset, source, target := testFoundryBundle(t)
	runs := make([][]byte, 0, 2)
	for _, content := range [][]byte{source, target} {
		records, err := foundryRecords(content)
		require.NoError(t, err)
		results, ok := records[0]["results"].([]any)
		require.True(t, ok)
		quality, ok := results[1].(map[string]any)
		require.True(t, ok)
		delete(quality, "passed")
		runs = append(runs, marshalJSONLines(t, records))
	}
	files := []mappingFile{
		mappingTestFile(t, 0, "dataset.jsonl", string(dataset)),
		mappingTestFile(t, 1, "source.jsonl", string(runs[0])),
		mappingTestFile(t, 2, "target.jsonl", string(runs[1])),
	}
	plan, _ := suggestMapping(files, "gpt-4.1-mini", "gpt-5.6-sol")
	require.Equal(t, "foundry", plan.Adapter)
	require.Contains(t, plan.Source.Evaluator.Filters,
		MappingFilter{Path: "/name", Value: "LFRSDisclosureQuality"})
	for _, adapter := range []string{"foundry", "generic"} {
		plan.Adapter = adapter
		preview, analysis := previewMapping(files, plan, "gpt-4.1-mini", "gpt-5.6-sol")
		require.True(t, preview.Valid, "%+v", preview.Issues)
		require.Equal(t, 1, preview.ComparableCount)
		require.Equal(t, 1, preview.UnclassifiedCount)
		require.Zero(t, analysis.Stable)
		require.Zero(t, analysis.Regressions)
		require.Equal(t, 1, analysis.PreExistingFailures)
	}
}

func TestNativeCanonicalValidatesAllReportedIdentities(t *testing.T) {
	tests := []struct {
		name   string
		first  string
		second string
		code   string
	}{
		{"later wrong variant", "", `,"model":"gpt-4o-mini"`, "MODEL_IDENTITY_CONFLICT"},
		{"run IDs without model", `,"run_id":"run-a"`, `,"run_id":"run-b"`, "AMBIGUOUS_PARTITION"},
		{"later correct model", "", `,"model":"gpt-4o"`, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows := make([]string, 0, 2)
			for index, identity := range []string{test.first, test.second} {
				rows = append(rows, fmt.Sprintf(
					`{"case_id":"%d","input":{"task":"Q"},"observations":`+
						`{"source":{"output":"A"%s},"target":{"output":"B"}},"assessments":[`+
						`{"evaluator":"quality","subject":"source","status":"pass"},`+
						`{"evaluator":"quality","subject":"target","status":"fail"}]}`, index, identity))
			}
			files := []mappingFile{mappingTestFile(t, 0, "paired.jsonl", rows[0]+"\n"+rows[1])}
			plan, _ := suggestMapping(files, "gpt-4o", "")
			preview, _ := previewMapping(files, plan, "gpt-4o", "")
			if test.code == "" {
				require.True(t, preview.Valid, "%+v", preview.Issues)
				require.Equal(t, "gpt-4o", preview.SourceModel)
			} else {
				require.False(t, preview.Valid)
				require.True(t, slices.ContainsFunc(preview.Issues, func(issue MappingIssue) bool {
					return issue.Code == test.code
				}), "%+v", preview.Issues)
			}
		})
	}
}

func TestPromptfooIndexedAndCompositeQualityRejection(t *testing.T) {
	tests := []struct {
		name       string
		assertion  string
		collection string
		status     string
		code       string
	}{
		{"indexed latency", `"assertion":{"type":"latency"}`, "", "/gradingResult/componentResults/0/pass", "NOT_QUALITY"},
		{"indexed cost", `"assertion":{"type":"cost"}`, "", "/gradingResult/componentResults/0/pass", "NOT_QUALITY"},
		{
			"selected composite",
			`"assertion":{"type":"assert-set"},"componentResults":[{"pass":false}]`,
			"/gradingResult/componentResults", "/pass", "COMPOSITE_EVALUATOR",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			content := fmt.Sprintf(`{"vars":{"case_id":"a"},"question":"Q","old":"A","new":"B","oldOK":true,
				"provider":{"id":"alias"},"response":{"output":"B"},
				"gradingResult":{"componentResults":[{"pass":false,%s}]}}`, test.assertion)
			files := []mappingFile{mappingTestFile(t, 0, "promptfoo.json", content)}
			plan := mappingTestPlan()
			plan.Source.CaseID, plan.Target.CaseID = "/vars/case_id", "/vars/case_id"
			plan.Target.Evaluator.Collection = test.collection
			plan.Target.Evaluator.Status = test.status
			preview, _ := previewMapping(files, plan, "", "")
			require.False(t, preview.Valid)
			require.True(t, slices.ContainsFunc(preview.Issues, func(issue MappingIssue) bool {
				return issue.Code == test.code
			}), "%+v", preview.Issues)
		})
	}
}
