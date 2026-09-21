// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOptionalEvidenceMappingRoundTripAndConflicts(t *testing.T) {
	content := `{"id":"a","question":{"task":"Q"},"old":"A","new":"B","oldOK":true,"newOK":false,
		"source_context":{"documents":["one"]},"target_context":["two"],
		"expected_output":{"answer":true},"expected_references":["r1"],
		"target_expected":{"answer":true},"target_references":["r1"],
		"source_score":1,"target_score":0,"source_reason":"source rationale","target_reason":"target rationale"}`
	file := mappingTestFile(t, 0, "evidence.json", content)
	dataset := mappingTestFile(t, 1, "dataset.json",
		`{"id":"a","question":{"task":"Q"},"expected":{"answer":true},"references":["r1"]}`)
	files := []mappingFile{file, dataset}
	plan := mappingTestPlan()
	plan.Source.Context, plan.Target.Context = "/source_context", "/target_context"
	plan.Source.ExpectedOutput, plan.Target.ExpectedOutput = "/expected_output", "/target_expected"
	plan.Source.ExpectedReferences, plan.Target.ExpectedReferences = "/expected_references", "/target_references"
	plan.Source.Evaluator.Score, plan.Target.Evaluator.Score = "/source_score", "/target_score"
	plan.Source.Evaluator.Rationale, plan.Target.Evaluator.Rationale = "/source_reason", "/target_reason"
	plan.Dataset = &MappingDataset{
		File: 1, CaseID: "/id", Input: "/question", ExpectedOutput: "/expected", Reference: "/references",
	}
	decoded, err := decodeEvaluationMapping(valueStringJSON(plan))
	require.NoError(t, err)
	require.Equal(t, plan, decoded)
	preview, analysis := previewMapping(files, decoded, "", "")
	require.True(t, preview.Valid, "%+v", preview.Issues)
	require.Len(t, analysis.Cases, 1)
	item := analysis.Cases[0]
	require.Equal(t, map[string]any{"documents": []any{"one"}}, item.SourceContext)
	require.Equal(t, []any{"two"}, item.TargetContext)
	require.Equal(t, map[string]any{"answer": true}, item.ExpectedOutput)
	require.Equal(t, []any{"r1"}, item.ExpectedReferences)
	require.Equal(t, new(1.0), item.SourceScore)
	require.Equal(t, new(0.0), item.TargetScore)
	require.Equal(t, "source rationale", item.SourceEvaluatorRationale)
	require.Equal(t, "target rationale", item.EvaluatorRationale)
	evidence := decodeOptimizationEvidence(t, optimizationTestInput(t, analysis))
	require.Equal(t, item.ExpectedReferences, evidence[0].ExpectedReferences)
	require.Equal(t, item.ExpectedOutput, evidence[0].ExpectedOutput)
	for _, test := range []struct {
		name, from, to string
		file           int
	}{
		{"lane output", `"target_expected":{"answer":true}`, `"target_expected":{"answer":false}`, 0},
		{"lane references", `"target_references":["r1"]`, `"target_references":["r2"]`, 0},
		{"dataset output", `"expected":{"answer":true}`, `"expected":{"answer":false}`, 1},
		{"dataset references", `"references":["r1"]`, `"references":["r2"]`, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := append([]mappingFile{}, files...)
			changed[test.file] = mappingTestFile(t, test.file, "changed.json",
				strings.ReplaceAll(string(files[test.file].content), test.from, test.to))
			preview, _ := previewMapping(changed, plan, "", "")
			require.False(t, preview.Valid)
			require.Contains(t, valueStringJSON(preview.Issues), "REFERENCE_CONFLICT")
		})
	}
	plan.Source.ExpectedReferences, plan.Target.ExpectedReferences = "", ""
	plan.Source.ExpectedOutput, plan.Target.ExpectedOutput = "", ""
	_, analysis = previewMapping(files, plan, "", "")
	require.Equal(t, []any{"r1"}, analysis.Cases[0].ExpectedReferences, "dataset references must not be dropped")
	require.Equal(t, map[string]any{"answer": true}, analysis.Cases[0].ExpectedOutput)
}

func TestOptionalEvidenceSuggestionsAndProposalValidation(t *testing.T) {
	file := mappingTestFile(t, 0, "data.json", `{
		"id":"a","question":"Q","source_output":"A","target_output":"B","source_pass":true,"target_pass":false,
		"source_context":"SC","target_context":"TC","expected_conclusion":"yes","expected_references":["r1"]}`)
	files := []mappingFile{file}
	plan, _ := suggestMapping(files, "", "")
	require.Equal(t, "/source_context", plan.Source.Context)
	require.Equal(t, "/target_context", plan.Target.Context)
	require.Equal(t, "/expected_conclusion", plan.Source.ExpectedOutput)
	require.Equal(t, "/expected_references", plan.Target.ExpectedReferences)
	schema := mappingStructuralProfile(files)
	plan.Source.Collection, plan.Target.Collection = "collection:0", "collection:0"
	require.NoError(t, validateMappingProposal(plan, schema))
	plan.Target.ExpectedReferences = "/not_supplied"
	require.ErrorContains(t, validateMappingProposal(plan, schema), "unknown field")
	old := mappingTestPlan()
	require.NotContains(t, valueStringJSON(old), `"context"`)
	require.NotContains(t, valueStringJSON(old), `"expectedOutput"`)
	require.NotContains(t, valueStringJSON(old), `"expectedReferences"`)
	oldFiles := []mappingFile{mappingTestFile(t, 0, "old.json",
		`{"id":"a","question":"Q","old":"A","new":"B","oldOK":true,"newOK":false}`)}
	preview, analysis := previewMapping(oldFiles, old, "", "")
	require.True(t, preview.Valid)
	require.Nil(t, analysis.Cases[0].SourceContext)
	require.Nil(t, analysis.Cases[0].TargetContext)
	require.Nil(t, analysis.Cases[0].ExpectedOutput)
	require.Nil(t, analysis.Cases[0].ExpectedReferences)
	require.Nil(t, analysis.Cases[0].SourceScore)
	require.Nil(t, analysis.Cases[0].TargetScore)
}

func TestOptimizationCarriesExplicitDataQualityWarnings(t *testing.T) {
	file := mappingTestFile(t, 0, "demo.json", `{
		"id":"a","question":"Requirement text","old":"A","new":"B","oldOK":true,"newOK":false,
		"migration_demo_provenance":{},"is_synthetic":true,"actual_context_available":false,
		"question_origin":"Requirement Description only; full runtime Question is unavailable"}`)
	preview, analysis := previewMapping([]mappingFile{file}, mappingTestPlan(), "", "")
	require.True(t, preview.Valid)
	input := optimizationTestInput(t, analysis)
	require.Contains(t, input.RequestedChanges, "outputs as synthetic")
	require.Contains(t, input.RequestedChanges, "runtime context is unavailable")
	require.Contains(t, input.RequestedChanges, "not the full runtime Question")
	require.Nil(t, analysis.Cases[0].SourceContext)
}

func TestCanonicalAndFoundryPreserveOptionalEvidence(t *testing.T) {
	record := `{"schema_version":"mme.case.v1","case_id":"a","input":{"task":"Full input"},
		"observations":{"source":{"output":"A","retrieved_context":{"document":"SC"}},
		"target":{"output":"B","retrieved_context":["TC"]}},
		"references":{"expected_output":{"answer":true},"expected_evidence":["r1"]},
		"assessments":[{"subject":"source","evaluator":"quality","status":"pass","score":1},
		{"subject":"target","evaluator":"quality","status":"fail","score":0}]}`
	analysis, err := analyzeEvaluation("canonical.json", []byte(record))
	require.NoError(t, err)
	files := []mappingFile{mappingTestFile(t, 0, "canonical.json", record)}
	plan, _ := suggestMapping(files, "", "")
	preview, mapped := previewMapping(files, plan, "", "")
	require.True(t, preview.Valid, "%+v", preview.Issues)
	require.Equal(t, analysis.Cases[0].SourceContext, mapped.Cases[0].SourceContext)
	require.Equal(t, analysis.Cases[0].TargetContext, mapped.Cases[0].TargetContext)
	require.Equal(t, map[string]any{"answer": true}, mapped.Cases[0].ExpectedOutput)
	require.Equal(t, []any{"r1"}, mapped.Cases[0].ExpectedReferences)
	require.Equal(t, new(1.0), mapped.Cases[0].SourceScore)
	require.Equal(t, new(0.0), mapped.Cases[0].TargetScore)
	plan.Source.Context, plan.Target.Context = "", ""
	plan.Source.ExpectedOutput, plan.Target.ExpectedOutput = "", ""
	plan.Source.ExpectedReferences, plan.Target.ExpectedReferences = "", ""
	preview, mapped = previewMapping(files, plan, "", "")
	require.True(t, preview.Valid, "old native maps remain compatible: %+v", preview.Issues)
	require.NotNil(t, mapped.Cases[0].ExpectedOutput)

	dataset, source, target := testFoundryBundle(t)
	datasetItems, err := parseFoundryDataset(dataset)
	require.NoError(t, err)
	foundry, err := analyzeFoundryEvaluationBundle("dataset.jsonl", dataset, "source.jsonl", source, "target.jsonl", target)
	require.NoError(t, err)
	files = []mappingFile{
		mappingTestFile(t, 0, "dataset.jsonl", string(dataset)),
		mappingTestFile(t, 1, "source.jsonl", string(source)),
		mappingTestFile(t, 2, "target.jsonl", string(target)),
	}
	plan, _ = suggestMapping(files, "gpt-4.1-mini", "gpt-5.6-sol")
	preview, mapped = previewMapping(files, plan, "gpt-4.1-mini", "gpt-5.6-sol")
	require.True(t, preview.Valid, "%+v", preview.Issues)
	for index, item := range mapped.Cases {
		require.Equal(t, datasetItems[item.CaseID].candidateResponse, item.ExpectedOutput)
		require.Equal(t, datasetItems[item.CaseID].query, item.Question)
		require.Equal(t, foundry.Cases[index].ExpectedOutput, item.ExpectedOutput)
		require.Nil(t, item.SourceContext, "Foundry context remains inside the full input, never fabricated")
	}
	encoded, err := json.Marshal(mapped.Cases)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"expectedOutput"`)
}
