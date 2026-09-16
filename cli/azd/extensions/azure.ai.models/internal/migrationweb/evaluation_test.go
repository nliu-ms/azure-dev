// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnalyzeEvaluationWorkbook(t *testing.T) {
	content := testEvaluationWorkbook(t)
	result, err := analyzeEvaluation("evaluation.xlsx", content)
	if err != nil {
		t.Fatal(err)
	}
	if result.CaseCount != 5 ||
		result.Stable != 1 ||
		result.Regressions != 1 ||
		result.Operational != 1 ||
		result.Improvements != 1 ||
		result.PreExistingFailures != 1 {
		t.Fatalf("unexpected analysis summary: %+v", result)
	}
	if len(result.Patterns) != 2 ||
		result.Patterns[0].Code != "operational_regression" ||
		result.Patterns[1].Code != "semantic_equivalence" {
		t.Fatalf("unexpected patterns: %+v", result.Patterns)
	}
	if len(result.Evaluators) != 1 ||
		result.Evaluators[0].Name != "conclusion-accuracy" ||
		result.Evaluators[0].SourcePassCount != 3 ||
		result.Evaluators[0].TargetPassCount != 3 {
		t.Fatalf("unexpected evaluator summary: %+v", result.Evaluators)
	}
}

func TestAnalyzeCanonicalJSONL(t *testing.T) {
	content := strings.Join([]string{
		`{"schema_version":"mme.case.v1","case_id":"case-1","input":{"task":"Question"},` +
			`"observations":{"source":{"output":"Identified"},"target":{"output":"Not Identified"}},` +
			`"assessments":[{"subject":"source","evaluator":"quality","status":"pass","score":1},` +
			`{"subject":"target","evaluator":"quality","status":"fail","score":0,` +
			`"rationale":"Target missed equivalent wording."}],` +
			`"failure":{"kind":"semantic_equivalence","detail":"Equivalent wording was rejected.",` +
			`"source":"customer"}}`,
	}, "\n")
	result, err := analyzeEvaluation("evaluation.jsonl", []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if result.Regressions != 1 ||
		len(result.Cases) != 1 ||
		result.Cases[0].EvaluatorRationale != "Target missed equivalent wording." {
		t.Fatalf("unexpected JSONL analysis: %+v", result)
	}
}

func TestEvaluationAnalysisHandler(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	prompt, err := writer.CreateFormFile("prompt", "prompt.txt")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := prompt.Write([]byte("Use supplied evidence only.")); err != nil {
		t.Fatal(err)
	}
	evaluation, err := writer.CreateFormFile("evaluation", "evaluation.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := evaluation.Write(testEvaluationWorkbook(t)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/evaluation-analysis", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	(&Server{}).handleEvaluationAnalysis(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}
	var result EvaluationAnalysis
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.CaseCount != 5 || result.PromptSHA256 == "" || result.EvaluationSHA256 == "" {
		t.Fatalf("unexpected analysis response: %+v", result)
	}
}

func testEvaluationWorkbook(t *testing.T) []byte {
	t.Helper()
	headers := []string{
		"Case ID",
		"Question",
		"Source Output",
		"Target Output",
		"Source Latency Ms",
		"Target Latency Ms",
		"Source Input Tokens",
		"Source Output Tokens",
		"Target Input Tokens",
		"Target Output Tokens",
		"Failure Kind",
		"Failure Detail",
		"Failure Source",
		"Source Conclusion Status",
		"Source Conclusion Score",
		"Target Conclusion Status",
		"Target Conclusion Score",
		"Target Conclusion Rationale",
	}
	rows := [][]string{
		headers,
		{
			"stable", "Stable case", "Identified", "Identified", "100", "105",
			"100", "20", "105", "20", "", "", "", "pass", "100", "pass", "100", "",
		},
		{
			"regression", "Regression case", "Identified", "Not Identified", "100", "110",
			"100", "20", "110", "20", "semantic_equivalence", "Equivalent wording rejected.",
			"customer", "pass", "100", "fail", "0", "Equivalent wording rejected.",
		},
		{
			"operational", "Operational case", "Identified", "Identified", "100", "150",
			"100", "20", "180", "40", "", "", "", "pass", "100", "pass", "100", "",
		},
		{
			"improvement", "Improvement case", "Not Identified", "Identified", "100", "105",
			"100", "20", "105", "20", "", "", "", "fail", "0", "pass", "100", "",
		},
		{
			"pre-existing", "Pre-existing case", "Not Identified", "Not Identified", "100", "105",
			"100", "20", "105", "20", "", "", "", "fail", "0", "fail", "0", "",
		},
	}

	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	writeZipFile(t, archive, "[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?>`+
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`+
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`+
		`<Default Extension="xml" ContentType="application/xml"/>`+
		`<Override PartName="/xl/workbook.xml" `+
		`ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>`+
		`<Override PartName="/xl/worksheets/sheet1.xml" `+
		`ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`+
		`</Types>`)
	writeZipFile(t, archive, "xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8"?>`+
		`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" `+
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`+
		`<sheets><sheet name="Evaluations" sheetId="1" r:id="rId1"/></sheets></workbook>`)
	writeZipFile(t, archive, "xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8"?>`+
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
		`<Relationship Id="rId1" `+
		`Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" `+
		`Target="worksheets/sheet1.xml"/></Relationships>`)

	var sheet strings.Builder
	sheet.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sheet.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for rowIndex, row := range rows {
		fmt.Fprintf(&sheet, `<row r="%d">`, rowIndex+1)
		for column, value := range row {
			reference := fmt.Sprintf("%s%d", excelColumn(column), rowIndex+1)
			fmt.Fprintf(
				&sheet,
				`<c r="%s" t="inlineStr"><is><t>%s</t></is></c>`,
				reference,
				xmlEscape(value),
			)
		}
		sheet.WriteString(`</row>`)
	}
	sheet.WriteString(`</sheetData></worksheet>`)
	writeZipFile(t, archive, "xl/worksheets/sheet1.xml", sheet.String())
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func writeZipFile(t *testing.T, archive *zip.Writer, name string, content string) {
	t.Helper()
	writer, err := archive.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
}

func excelColumn(index int) string {
	var result string
	for index >= 0 {
		result = string(rune('A'+index%26)) + result
		index = index/26 - 1
	}
	return result
}

func xmlEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, `"`, "&quot;")
	return strings.ReplaceAll(value, "'", "&apos;")
}
