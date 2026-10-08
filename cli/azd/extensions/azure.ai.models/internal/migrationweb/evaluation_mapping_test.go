// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func mappingTestFile(t *testing.T, index int, name, content string) mappingFile {
	t.Helper()
	file, err := profileMappingFile(index, name, []byte(content))
	require.NoError(t, err)
	return file
}

func mappingTestLane(file int, output, status string) MappingLane {
	lane := emptyMappingLane()
	lane.File, lane.CaseID, lane.Input, lane.Output = file, "/id", "/question", output
	lane.Evaluator.Status = status
	return lane
}

func mappingTestPlan() EvaluationMapping {
	return EvaluationMapping{
		Version: 1, Adapter: "generic",
		Source: mappingTestLane(0, "/old", "/oldOK"), Target: mappingTestLane(0, "/new", "/newOK"),
	}
}

func TestReviewedMappingLayouts(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		exts  []string
		plan  func() EvaluationMapping
	}{
		{
			name: "unfamiliar combined columns",
			files: []string{
				`[{"id":"a","question":"Q","old":"A","new":"B","oldOK":true,"newOK":false},` +
					`{"id":"b","question":"R","old":"C","new":{"answer":"D"},"oldOK":false,"newOK":true}]`,
			},
			exts: []string{"json"}, plan: mappingTestPlan,
		},
		{
			name: "split JSONL and reordered CSV",
			files: []string{
				"{\"id\":\"a\",\"question\":\"Q\",\"old\":\"A\",\"oldOK\":true}\n" +
					"{\"id\":\"b\",\"question\":\"R\",\"old\":\"C\",\"oldOK\":false}",
				"id,question,new,newOK\nb,R,D,true\na,Q,B,false\n",
			},
			exts: []string{"jsonl", "csv"},
			plan: func() EvaluationMapping {
				plan := mappingTestPlan()
				plan.Target.File = 1
				return plan
			},
		},
		{
			name: "combined long partitions",
			files: []string{`[
				{"id":"b","question":"R","answer":"D","pass":true,"model":"target"},
				{"id":"a","question":"Q","answer":"A","pass":true,"model":"source"},
				{"id":"b","question":"R","answer":"C","pass":false,"model":"source"},
				{"id":"a","question":"Q","answer":"B","pass":false,"model":"target"},
				{"id":"a","question":"Q","answer":"E","pass":false,"model":"other"}]`},
			exts: []string{"json"},
			plan: func() EvaluationMapping {
				plan := mappingTestPlan()
				for _, lane := range []*MappingLane{&plan.Source, &plan.Target} {
					lane.Output, lane.Evaluator.Status, lane.Model = "/answer", "/pass", "/model"
				}
				plan.Source.Filters = []MappingFilter{{Path: "/model", Value: "source"}}
				plan.Target.Filters = []MappingFilter{{Path: "/model", Value: "target"}}
				return plan
			},
		},
		{
			name: "nested collections",
			files: []string{`{"runs":{"s":[
				{"id":"a","question":"Q","old":"A","oldOK":true},
				{"id":"b","question":"R","old":"C","oldOK":false}],
				"t":[{"id":"b","question":"R","new":"D","newOK":true},
				{"id":"a","question":"Q","new":"B","newOK":false}]}}`},
			exts: []string{"json"},
			plan: func() EvaluationMapping {
				plan := mappingTestPlan()
				plan.Source.Collection, plan.Target.Collection = "/runs/s", "/runs/t"
				return plan
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var files []mappingFile
			for i, content := range test.files {
				files = append(files, mappingTestFile(t, i, "arbitrary."+test.exts[i], content))
			}
			preview, analysis := previewMapping(files, test.plan(), "source", "target")
			require.True(t, preview.Valid, "%+v", preview.Issues)
			require.Equal(t, 2, preview.CaseCount)
			require.Equal(t, 2, preview.ComparableCount)
			require.Equal(t, 1, analysis.Regressions)
			require.Equal(t, 1, analysis.Improvements)
			require.Equal(t, "a", preview.Cases[0].CaseID)
			require.Equal(t, "B", preview.Cases[0].TargetOutput)
		})
	}
}

func TestReviewedMappingValidation(t *testing.T) {
	base := `[{"id":"a","question":"Q","old":"A","new":"B","oldOK":true,"newOK":false}]`
	tests := []struct {
		name    string
		content string
		edit    func(*EvaluationMapping)
		code    string
	}{
		{"duplicate", strings.Replace(base, "}]",
			`},{"id":"a","question":"Q","old":"A","new":"B","oldOK":true,"newOK":true}]`, 1),
			nil, "DUPLICATE_CASE_KEY"},
		{"wrong model", strings.Replace(base, `"id":"a"`, `"id":"a","model":"wrong"`, 1),
			nil, "MODEL_IDENTITY_CONFLICT"},
		{"wrong ID type", strings.Replace(base, `"id":"a"`, `"id":false`, 1), nil, "MISSING_CASE_ID"},
		{"wrong grade type", strings.Replace(base, `"newOK":false`, `"newOK":{}`, 1), nil, "INVALID_GRADE"},
		{"bad status", strings.Replace(base, `"newOK":false`, `"newOK":"perhaps"`, 1), nil, "INVALID_GRADE"},
		{"missing output", strings.Replace(base, `,"new":"B"`, "", 1), nil, "INVALID_MAPPING"},
		{"missing stable ID map", base, func(plan *EvaluationMapping) { plan.Source.CaseID = "" }, "INVALID_MAPPING"},
		{"bad pointer", base, func(plan *EvaluationMapping) { plan.Source.CaseID = "/~9" }, "INVALID_MAPPING"},
		{"unknown grade path", base, func(plan *EvaluationMapping) {
			plan.Target.Evaluator.Status = "/typo"
		}, "MISSING_PASS_RULE"},
		{"missing threshold", base, func(plan *EvaluationMapping) {
			plan.Target.PassRule, plan.Target.Evaluator.Score = "threshold", "/newOK"
		}, "INVALID_MAPPING"},
		{"identical lanes", base, func(plan *EvaluationMapping) { plan.Target = plan.Source }, "SAME_OBSERVATION"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := mappingTestPlan()
			if test.edit != nil {
				test.edit(&plan)
			}
			files := []mappingFile{mappingTestFile(t, 0, "data.json", test.content)}
			preview, _ := previewMapping(files, plan, "source", "target")
			require.False(t, preview.Valid)
			require.True(t, slices.ContainsFunc(preview.Issues, func(issue MappingIssue) bool {
				return issue.Code == test.code
			}), "%+v", preview.Issues)
		})
	}
}

func TestReviewedMappingMissingGradesAndThresholds(t *testing.T) {
	for _, rule := range []string{"reported", "threshold"} {
		t.Run(rule, func(t *testing.T) {
			files := []mappingFile{mappingTestFile(t, 0, "data.json", `[
				{"id":"a","question":"Q","old":"A","new":"B","oldOK":true,"newOK":false,"score":0},
				{"id":"b","question":"Q","old":"A","new":"B","oldOK":true,"newOK":null,"score":null},
				{"id":"c","question":"Q","old":"A","new":"B","oldOK":true},
				{"id":"d","question":"Q","old":"A","new":"B","oldOK":true,"newOK":"error"},
				{"id":"e","question":"Q","old":"A","new":"B","oldOK":true,"newOK":true,"score":1}]`)}
			plan := mappingTestPlan()
			plan.Target.Evaluator.Score = "/score"
			plan.Target.PassRule = rule
			if rule == "threshold" {
				plan.Target.Threshold = new(0.5)
			}
			preview, analysis := previewMapping(files, plan, "", "")
			require.True(t, preview.Valid, "%+v", preview.Issues)
			require.Equal(t, 5, preview.CaseCount)
			require.Equal(t, 2, preview.ComparableCount)
			require.Equal(t, 3, preview.UnclassifiedCount)
			require.Equal(t, "fail", preview.Cases[0].TargetStatus)
			require.Equal(t, new(0.0), preview.Cases[0].TargetScore)
			require.Equal(t, "unknown", preview.Cases[1].TargetStatus)
			require.Equal(t, 1, analysis.Regressions)
			require.Equal(t, 0.5, analysis.Evaluators[0].TargetPassRate)
			require.Equal(t, 1.0, analysis.Evaluators[0].SourcePassRate)
		})
	}
}

func TestReviewedMappingSplitConflictsAndDataset(t *testing.T) {
	source := `{"id":"a","question":"Q","old":"A","oldOK":true}`
	for _, test := range []struct {
		name, target, code string
	}{
		{"input", `{"id":"a","question":"different","new":"B","newOK":false}`, "INPUT_CONFLICT"},
		{"unmatched", `{"id":"b","question":"Q","new":"B","newOK":false}`, "UNMATCHED_CASE"},
		{"typed key", `{"id":1,"question":"Q","new":"B","newOK":false}`, "UNMATCHED_CASE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := []mappingFile{
				mappingTestFile(t, 0, "s.jsonl", source),
				mappingTestFile(t, 1, "t.jsonl", test.target),
			}
			plan := mappingTestPlan()
			plan.Target.File = 1
			preview, _ := previewMapping(files, plan, "", "")
			require.False(t, preview.Valid)
			require.True(t, slices.ContainsFunc(preview.Issues, func(issue MappingIssue) bool {
				return issue.Code == test.code
			}), "%+v", preview.Issues)
		})
	}
	files := []mappingFile{
		mappingTestFile(t, 0, "s.jsonl", source),
		mappingTestFile(t, 1, "t.jsonl", `{"id":"a","new":"B","newOK":false}`),
		mappingTestFile(t, 2, "d.csv", "id,question,reference\na,Q,expected\n"),
	}
	plan := mappingTestPlan()
	plan.Target.File, plan.Target.Input = 1, ""
	plan.Dataset = &MappingDataset{File: 2, CaseID: "/id", Input: "/question", Reference: "/reference"}
	preview, _ := previewMapping(files, plan, "", "")
	require.True(t, preview.Valid, "%+v", preview.Issues)
	require.Equal(t, "Q", preview.Cases[0].Input)
}

func TestReviewedMappingLiteralKeysAndLargeIDs(t *testing.T) {
	files := []mappingFile{mappingTestFile(t, 0, "data.json", `[
		{"id":9007199254740993,"question":"Q","sample.output":"A","a/b~c":"B","oldOK":true,"newOK":false}]`)}
	plan := mappingTestPlan()
	plan.Source.Output, plan.Target.Output = "/sample.output", "/a~1b~0c"
	preview, _ := previewMapping(files, plan, "", "")
	require.True(t, preview.Valid, "%+v", preview.Issues)
	require.Equal(t, "9007199254740993", preview.Cases[0].CaseID)
	require.Equal(t, "A", preview.Cases[0].SourceOutput)
	require.Equal(t, "B", preview.Cases[0].TargetOutput)
}

func TestReviewedMappingPromptfooQualityAndTokens(t *testing.T) {
	content := `{"results":{"version":3,"results":[
		{"provider":{"id":"alias-source"},"vars":{"case_id":"a","question":"Q"},"success":true,
		 "response":{"output":"A","tokenUsage":{"prompt":10,"completion":2}},
		 "tokenUsage":{"prompt":999,"completion":999},
		 "gradingResult":{"pass":true,"componentResults":[
			{"assertion":{"type":"llm-rubric"},"pass":true,"score":1},
			{"assertion":{"type":"latency"},"pass":true}]}},
		{"provider":{"id":"alias-target"},"vars":{"case_id":"a","question":"Q"},"success":false,
		 "failureReason":1,"error":"latency assertion failed",
		 "response":{"output":{"answer":"B"},"tokenUsage":{"prompt":10,"completion":2}},
		 "tokenUsage":{"prompt":1999,"completion":1999},
		 "gradingResult":{"pass":false,"componentResults":[
			{"assertion":{"type":"latency"},"pass":false},
			{"assertion":{"type":"llm-rubric"},"pass":true,"score":1}]}}]}}`
	files := []mappingFile{mappingTestFile(t, 0, "run.json", content)}
	plan, _ := suggestMapping(files, "source", "target")
	for _, lane := range []*MappingLane{&plan.Source, &plan.Target} {
		lane.Evaluator.Filters = []MappingFilter{{Path: "/assertion/type", Value: "llm-rubric"}}
		lane.Evaluator.Rationale = ""
	}
	plan.Source.Filters = []MappingFilter{{Path: "/provider/id", Value: "alias-source"}}
	plan.Target.Filters = []MappingFilter{{Path: "/provider/id", Value: "alias-target"}}
	preview, analysis := previewMapping(files, plan, "source", "target")
	require.True(t, preview.Valid, "%+v", preview.Issues)
	require.Equal(t, 1, analysis.Stable)
	require.Zero(t, analysis.Regressions)
	require.Zero(t, analysis.Operational)
	require.Empty(t, preview.SourceModel)
	require.Equal(t, `{"answer":"B"}`, preview.Cases[0].TargetOutput)
	ambiguous := mappingTestFile(t, 0, "run.json",
		strings.ReplaceAll(content, `"type":"latency"`, `"type":"llm-rubric"`))
	ambiguousPreview, _ := previewMapping([]mappingFile{ambiguous}, plan, "", "")
	require.False(t, ambiguousPreview.Valid)
	require.True(t, slices.ContainsFunc(ambiguousPreview.Issues, func(issue MappingIssue) bool {
		return issue.Code == "AMBIGUOUS_EVALUATOR"
	}))
	plan.Target.Evaluator = MappingEvaluator{Status: "/success", Filters: []MappingFilter{}}
	preview, _ = previewMapping(files, plan, "", "")
	require.False(t, preview.Valid)
	require.True(t, slices.ContainsFunc(preview.Issues, func(issue MappingIssue) bool {
		return issue.Code == "MIXED_AGGREGATE"
	}))
}

func TestReviewedMappingNativeAdapters(t *testing.T) {
	t.Run("Meera compatibility and fixed plan", func(t *testing.T) {
		file, err := profileMappingFile(0, "workbook.xlsx", testEvaluationWorkbook(t))
		require.NoError(t, err)
		files := []mappingFile{file}
		plan, _ := suggestMapping(files, "", "")
		require.Equal(t, "meera", plan.Adapter)
		preview, analysis := previewMapping(files, plan, "", "")
		require.True(t, preview.Valid, "%+v", preview.Issues)
		require.Equal(t, 5, preview.CaseCount)
		require.Equal(t, 1, analysis.Regressions)
		require.Equal(t, 1, analysis.Improvements)
		plan.Target.Evaluator.Status = "/Source Conclusion Status"
		preview, _ = previewMapping(files, plan, "", "")
		require.False(t, preview.Valid)
		require.Equal(t, "NATIVE_MAPPING_EDIT", preview.Issues[0].Code)
	})
	t.Run("Foundry content roles and strict dataset", func(t *testing.T) {
		dataset, source, target := testFoundryBundle(t)
		files := []mappingFile{
			mappingTestFile(t, 0, "anything.jsonl", string(target)),
			mappingTestFile(t, 1, "anything.jsonl", string(dataset)),
			mappingTestFile(t, 2, "anything.jsonl", string(source)),
		}
		plan, _ := suggestMapping(files, "gpt-4.1-mini", "gpt-5.6-sol")
		require.Equal(t, "foundry", plan.Adapter)
		require.Equal(t, 2, plan.Source.File)
		require.Equal(t, 0, plan.Target.File)
		require.Equal(t, 1, plan.Dataset.File)
		preview, analysis := previewMapping(files, plan, "gpt-4.1-mini", "gpt-5.6-sol")
		require.True(t, preview.Valid, "%+v", preview.Issues)
		require.Equal(t, 2, preview.CaseCount)
		require.Equal(t, 1, analysis.Regressions)
		require.Equal(t, 1, analysis.PreExistingFailures)
		files[1].content = bytes.ReplaceAll(dataset, []byte(`"query":`), []byte(`"different":`))
		preview, _ = previewMapping(files, plan, "gpt-4.1-mini", "gpt-5.6-sol")
		require.False(t, preview.Valid)
	})
}

func mappingTestRequest(
	t *testing.T, path string, files []mappingFile, plan EvaluationMapping, fields map[string]string,
) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, file := range files {
		writeMultipartFile(t, writer, "files", file.profile.Name, file.content)
	}
	prompt := "Source prompt"
	if value, exists := fields["promptContent"]; exists {
		prompt = value
	}
	writeMultipartFile(t, writer, "prompt", "prompt.txt", []byte(prompt))
	require.NoError(t, writer.WriteField("mapping", valueStringJSON(plan)))
	for name, value := range fields {
		require.NoError(t, writer.WriteField(name, value))
	}
	require.NoError(t, writer.Close())
	request := httptest.NewRequest(http.MethodPost, path, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func TestReviewedMappingHTTPConfirmationAndOptimization(t *testing.T) {
	content := `{"schema_version":"mme.case.v1","case_id":"a","input":{"task":"Q"},
		"observations":{"source":{"output":"A"},"target":{"output":"B"}},
		"assessments":[{"evaluator":"quality","subject":"source","status":"pass"},
		{"evaluator":"quality","subject":"target","status":"fail","rationale":"PRIVATE_RATIONALE"}],
		"failure":{"kind":"semantic_equivalence","detail":"PRIVATE_DETAIL","source":"customer"}}`
	files := []mappingFile{mappingTestFile(t, 0, "evaluation.json", content)}
	plan, _ := suggestMapping(files, "", "")
	optimizer := &recordingPromptOptimizer{}
	server := &Server{promptOptimizer: optimizer}
	response := httptest.NewRecorder()
	server.handleMappingRequest(response, mappingTestRequest(t, "/api/evaluation-mapping/preview", files, plan, nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var preview MappingPreview
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &preview))
	require.True(t, preview.Valid, "%+v", preview.Issues)
	fields := map[string]string{
		"mappingConfirmed": "true", "evaluationSha256": preview.EvaluationSHA256,
		"optimizerAccountName": "account", "optimizerModelName": "model", "optimizerDeploymentName": "deployment",
		"caseCount": "99999", "regressions": "99999",
	}
	fields["allowEvaluationContent"] = "true"
	for _, endpoint := range []string{"/api/evaluation-analysis", "/api/prompt-optimization"} {
		t.Run(endpoint, func(t *testing.T) {
			call := server.handleEvaluationAnalysis
			if endpoint == "/api/prompt-optimization" {
				call = server.handlePromptOptimization
			}
			response := httptest.NewRecorder()
			call(response, mappingTestRequest(t, endpoint, files, plan, fields))
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.NotContains(t, optimizer.input.RequestedChanges, "PRIVATE_DETAIL")
			if endpoint == "/api/prompt-optimization" {
				require.Contains(t, optimizer.input.RequestedChanges, "PRIVATE_RATIONALE")
			}
			require.NotContains(t, optimizer.input.RequestedChanges, "99999")
			if endpoint == "/api/evaluation-analysis" {
				var analysis EvaluationAnalysis
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &analysis))
				require.Equal(t, 1, analysis.CaseCount)
				require.Equal(t, 1, analysis.Regressions)
			}
			for _, change := range []string{"mapping", "prompt", "binding", "confirmation"} {
				edited := plan
				changed := make(map[string]string, len(fields))
				for name, value := range fields {
					changed[name] = value
				}
				switch change {
				case "mapping":
					edited.Adapter = "generic"
				case "prompt":
					changed["promptContent"] = "changed prompt"
				case "binding":
					changed["targetModelName"] = "changed-model"
				case "confirmation":
					changed["mappingConfirmed"] = "false"
				}
				response = httptest.NewRecorder()
				call(response, mappingTestRequest(t, endpoint, files, edited, changed))
				require.Equal(t, http.StatusBadRequest, response.Code, "%s: %s", change, response.Body.String())
			}
		})
	}
}

func TestReviewedMappingProfileBoundsAndUnknownRoles(t *testing.T) {
	t.Run("duplicate CSV headers", func(t *testing.T) {
		file := mappingTestFile(t, 0, "data.csv", "id,Status,Status\nx,pass,fail\n")
		require.Equal(t, "pass", file.collections[""][0]["[2] Status"])
		require.Equal(t, "fail", file.collections[""][0]["[3] Status"])
	})
	t.Run("unknown files do not establish roles", func(t *testing.T) {
		files := []mappingFile{
			mappingTestFile(t, 0, "SOURCE.jsonl", `{"id":"a","input":"Q","output":"A","pass":true}`),
			mappingTestFile(t, 1, "TARGET.jsonl", `{"id":"a","input":"Q","output":"B","pass":false}`),
		}
		plan, _ := suggestMapping(files, "", "")
		require.Equal(t, -1, plan.Source.File)
		require.Equal(t, -1, plan.Target.File)
	})
}

func TestReviewedMappingThresholdDirectionsAndConflicts(t *testing.T) {
	for _, test := range []struct {
		name, operator, reported, wantStatus string
		score                                float64
		valid                                bool
	}{
		{"gte fails zero", "gte", "", "fail", 0, true},
		{"gte boundary", "gte", "", "pass", 0.5, true},
		{"lte boundary", "lte", "", "pass", 0.5, true},
		{"lte fail", "lte", "", "fail", 1, true},
		{"reported contradiction", "gte", "fail", "pass", 1, false},
		{"error overrides score", "gte", "error", "error", 1, true},
		{"skipped overrides score", "gte", "skipped", "skipped", 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := map[string]any{
				"id": "a", "question": "Q", "old": "A", "new": "B", "oldOK": true,
				"newOK": test.reported, "score": test.score,
			}
			// Keep a second comparable pair so an error/skipped first row remains previewable.
			second := map[string]any{
				"id": "b", "question": "Q", "old": "A", "new": "B", "oldOK": true, "newOK": "", "score": 0.5,
			}
			file := mappingTestFile(t, 0, "data.json", valueStringJSON([]any{row, second}))
			plan := mappingTestPlan()
			plan.Target.PassRule, plan.Target.Operator, plan.Target.Threshold = "threshold", test.operator, new(0.5)
			plan.Target.Evaluator.Score = "/score"
			preview, _ := previewMapping([]mappingFile{file}, plan, "", "")
			require.Equal(t, test.valid, preview.Valid, "%+v", preview.Issues)
			require.Equal(t, test.wantStatus, preview.Cases[0].TargetStatus)
		})
	}
}

func TestReviewedMappingHardLimits(t *testing.T) {
	t.Run("full lane record limit", func(t *testing.T) {
		rows := make([]map[string]any, maxEvaluationRows+1)
		for index := range rows {
			rows[index] = map[string]any{
				"id": strconv.Itoa(index), "question": "Q", "old": "A", "new": "B", "oldOK": true, "newOK": false,
			}
		}
		file := mappingTestFile(t, 0, "data.json", valueStringJSON(rows))
		preview, _ := previewMapping([]mappingFile{file}, mappingTestPlan(), "", "")
		require.False(t, preview.Valid)
		require.True(t, slices.ContainsFunc(preview.Issues, func(issue MappingIssue) bool {
			return issue.Code == "ROW_LIMIT"
		}))
	})
	t.Run("raw record limit", func(t *testing.T) {
		_, err := profileMappingFile(0, "data.jsonl", []byte(strings.Repeat("{\"id\":\"a\"}\n", maxMappingRecords+1)))
		require.ErrorContains(t, err, "20,000")
	})
	t.Run("total upload size", func(t *testing.T) {
		file := mappingFile{
			profile: MappingFileProfile{Name: "data.json"},
			content: bytes.Repeat([]byte(" "), maxEvaluationUploadBytes),
		}
		response := httptest.NewRecorder()
		(&Server{}).handleMappingRequest(response,
			mappingTestRequest(t, "/api/evaluation-mapping/profile", []mappingFile{file}, mappingTestPlan(), nil))
		require.Equal(t, http.StatusBadRequest, response.Code)
	})
}

func TestReviewedMappingWorkbookCollections(t *testing.T) {
	var content bytes.Buffer
	archive := zip.NewWriter(&content)
	writeZipFile(t, archive, "xl/workbook.xml",
		`<workbook xmlns:r="urn:r"><sheets><sheet name="First" r:id="r1"/>`+
			`<sheet name="Second~sheet" r:id="r2"/></sheets></workbook>`)
	writeZipFile(t, archive, "xl/_rels/workbook.xml.rels",
		`<Relationships><Relationship Id="r1" Target="worksheets/s1.xml"/>`+
			`<Relationship Id="r2" Target="worksheets/s2.xml"/></Relationships>`)
	sheet := `<worksheet><sheetData><row>` +
		`<c r="A1" t="inlineStr"><is><t>id</t></is></c>` +
		`<c r="B1" t="inlineStr"><is><t>Status</t></is></c>` +
		`<c r="C1" t="inlineStr"><is><t>Status</t></is></c></row><row>` +
		`<c r="A2"><v>1</v></c><c r="B2" t="b"><v>0</v></c>` +
		`<c r="C2" t="b"><v>1</v></c></row></sheetData></worksheet>`
	writeZipFile(t, archive, "xl/worksheets/s1.xml", sheet)
	writeZipFile(t, archive, "xl/worksheets/s2.xml", sheet)
	require.NoError(t, archive.Close())
	file, err := profileMappingFile(0, "data.xlsx", content.Bytes())
	require.NoError(t, err)
	require.Len(t, file.profile.Collections, 2)
	require.Equal(t, "false", file.collections["/sheets/Second~0sheet"][0]["[2] Status"])
	require.Equal(t, "true", file.collections["/sheets/First"][0]["[3] Status"])
}
func TestReviewedMappingAdditionalProfileBounds(t *testing.T) {
	t.Run("file count", func(t *testing.T) {
		file := mappingTestFile(t, 0, "data.json", `[{"id":"a"}]`)
		for _, files := range [][]mappingFile{nil, {file, file, file, file}} {
			response := httptest.NewRecorder()
			(&Server{}).handleMappingRequest(response,
				mappingTestRequest(t, "/api/evaluation-mapping/profile", files, mappingTestPlan(), nil))
			require.Equal(t, http.StatusBadRequest, response.Code)
		}
	})
	t.Run("full data validation beyond preview", func(t *testing.T) {
		rows := make([]map[string]any, 21)
		for index := range rows {
			rows[index] = map[string]any{
				"id": string(rune('a' + index)), "question": "Q", "old": "A", "new": "B",
				"oldOK": true, "newOK": false,
			}
		}
		rows[20]["newOK"] = []any{true}
		file := mappingTestFile(t, 0, "data.json", valueStringJSON(rows))
		preview, _ := previewMapping([]mappingFile{file}, mappingTestPlan(), "", "")
		require.False(t, preview.Valid)
		require.Len(t, preview.Cases, 10)
		require.Equal(t, 21, preview.CaseCount)
	})
	t.Run("nesting bound", func(t *testing.T) {
		content := strings.Repeat(`{"x":`, maxMappingDepth+1) + "true" + strings.Repeat("}", maxMappingDepth+1)
		_, err := profileMappingFile(0, "deep.json", []byte(content))
		require.Error(t, err)
	})
}

func TestReviewedMappingModelVersionsDoNotHideVariants(t *testing.T) {
	for _, test := range []struct {
		actual, expected string
		compatible       bool
	}{
		{"gpt-4o-2024-08-06", "gpt-4o", true},
		{"gpt-5.6-sol-2026-07-09", "gpt-5.6-sol", true},
		{"gpt-4o-mini", "gpt-4o", false},
		{"gpt-5.4-mini", "gpt-5.4", false},
		{"gpt-5.4", "gpt-5.6-sol", false},
	} {
		t.Run(test.actual+"/"+test.expected, func(t *testing.T) {
			require.Equal(t, test.compatible, modelNamesCompatible(test.actual, test.expected))
		})
	}
}

func TestReviewedMappingErrorWithoutOutputIsUnclassified(t *testing.T) {
	file := mappingTestFile(t, 0, "data.json", `[
		{"id":"a","question":"Q","old":"A","new":"B","oldOK":true,"newOK":false},
		{"id":"b","question":"Q","old":"A","oldOK":true,"newOK":"error"}]`)
	preview, analysis := previewMapping([]mappingFile{file}, mappingTestPlan(), "", "")
	require.True(t, preview.Valid, "%+v", preview.Issues)
	require.Equal(t, 2, preview.CaseCount)
	require.Equal(t, 1, preview.ComparableCount)
	require.Equal(t, 1, preview.UnclassifiedCount)
	require.Equal(t, 1, analysis.Regressions)
	require.Equal(t, "error", preview.Cases[1].TargetStatus)
}
