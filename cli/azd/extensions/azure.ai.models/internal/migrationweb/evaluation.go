// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	maxEvaluationRows        = 10_000
	maxWorkbookExpandedBytes = 64 * 1024 * 1024
)

// EvaluationAnalysis contains deterministic Source-to-Target regression analysis.
type EvaluationAnalysis struct {
	FileName            string              `json:"fileName"`
	Format              string              `json:"format"`
	SheetName           string              `json:"sheetName,omitempty"`
	SourceModel         string              `json:"sourceModel,omitempty"`
	TargetModel         string              `json:"targetModel,omitempty"`
	SourceRunID         string              `json:"sourceRunId,omitempty"`
	TargetRunID         string              `json:"targetRunId,omitempty"`
	EvaluationID        string              `json:"evaluationId,omitempty"`
	PromptSHA256        string              `json:"promptSha256"`
	EvaluationSHA256    string              `json:"evaluationSha256"`
	CaseCount           int                 `json:"caseCount"`
	Stable              int                 `json:"stable"`
	Regressions         int                 `json:"regressions"`
	Operational         int                 `json:"operationalRegressions"`
	Improvements        int                 `json:"improvements"`
	PreExistingFailures int                 `json:"preExistingFailures"`
	DetectedFields      []string            `json:"detectedFields"`
	Evaluators          []EvaluatorSummary  `json:"evaluators"`
	Patterns            []RegressionPattern `json:"patterns"`
	Cases               []RegressionCase    `json:"cases"`
	Warnings            []string            `json:"warnings,omitempty"`
}

// EvaluatorSummary compares pass rates and scores for one evaluator.
type EvaluatorSummary struct {
	Name               string   `json:"name"`
	SourcePassCount    int      `json:"sourcePassCount"`
	TargetPassCount    int      `json:"targetPassCount"`
	SourcePassRate     float64  `json:"sourcePassRate"`
	TargetPassRate     float64  `json:"targetPassRate"`
	SourceAverageScore *float64 `json:"sourceAverageScore,omitempty"`
	TargetAverageScore *float64 `json:"targetAverageScore,omitempty"`
}

// RegressionPattern groups cases that share a customer-provided or inferred failure category.
type RegressionPattern struct {
	Code          string   `json:"code"`
	Label         string   `json:"label"`
	Count         int      `json:"count"`
	Prevalence    float64  `json:"prevalence"`
	CaseIDs       []string `json:"caseIds"`
	PromptFixable string   `json:"promptFixable"`
}

// RegressionCase describes one Target quality failure or operational regression.
type RegressionCase struct {
	CaseID              string   `json:"caseId"`
	Question            string   `json:"question,omitempty"`
	Outcome             string   `json:"outcome"`
	SourceStatus        string   `json:"sourceStatus"`
	TargetStatus        string   `json:"targetStatus"`
	SourceOutput        string   `json:"sourceOutput,omitempty"`
	TargetOutput        string   `json:"targetOutput,omitempty"`
	EvaluatorRationale  string   `json:"evaluatorRationale,omitempty"`
	FailureKind         string   `json:"failureKind"`
	FailureDetail       string   `json:"failureDetail,omitempty"`
	ExpectedReasoning   string   `json:"expectedReasoning,omitempty"`
	SupportingEvidence  []string `json:"supportingEvidence,omitempty"`
	FailureSource       string   `json:"failureSource,omitempty"`
	PromptFixable       string   `json:"promptFixable"`
	Confidence          string   `json:"confidence"`
	LatencyDeltaPercent *float64 `json:"latencyDeltaPercent,omitempty"`
	TokenDeltaPercent   *float64 `json:"tokenDeltaPercent,omitempty"`
}

type evaluationCase struct {
	caseID             string
	question           string
	sourceOutput       string
	targetOutput       string
	sourceLatency      *float64
	targetLatency      *float64
	sourceInputTokens  *float64
	sourceOutputTokens *float64
	targetInputTokens  *float64
	targetOutputTokens *float64
	failureKind        string
	failureDetail      string
	expectedReasoning  string
	supportingEvidence []string
	failureSource      string
	assessments        []evaluationAssessment
}

type evaluationAssessment struct {
	evaluator string
	subject   string
	status    string
	score     *float64
	rationale string
}

type foundryDatasetItem struct {
	caseID            string
	query             string
	description       string
	candidateResponse string
}

type foundryRunItem struct {
	dataset      foundryDatasetItem
	output       string
	model        string
	runID        string
	evaluationID string
	latency      *float64
	inputTokens  *float64
	outputTokens *float64
	assessments  []evaluationAssessment
}

type worksheetData struct {
	name    string
	headers []string
	rows    []map[string]string
}

type xlsxWorkbook struct {
	Sheets []xlsxSheet `xml:"sheets>sheet"`
}

type xlsxSheet struct {
	Name  string `xml:"name,attr"`
	RelID string `xml:"id,attr"`
}

type xlsxRelationships struct {
	Relationships []xlsxRelationship `xml:"Relationship"`
}

type xlsxRelationship struct {
	ID     string `xml:"Id,attr"`
	Target string `xml:"Target,attr"`
}

type xlsxWorksheet struct {
	Rows []xlsxRow `xml:"sheetData>row"`
}

type xlsxRow struct {
	Cells []xlsxCell `xml:"c"`
}

type xlsxCell struct {
	Reference string         `xml:"r,attr"`
	Type      string         `xml:"t,attr"`
	Value     string         `xml:"v"`
	Inline    xlsxInlineText `xml:"is"`
}

type xlsxInlineText struct {
	Text string `xml:"t"`
}

type xlsxSharedStrings struct {
	Items []xlsxSharedString `xml:"si"`
}

type xlsxSharedString struct {
	Text string    `xml:"t"`
	Runs []xlsxRun `xml:"r"`
}

type xlsxRun struct {
	Text string `xml:"t"`
}

func analyzeEvaluation(fileName string, content []byte) (EvaluationAnalysis, error) {
	extension := strings.ToLower(filepath.Ext(fileName))
	var cases []evaluationCase
	var fields []string
	var sheetName string
	var err error

	switch extension {
	case ".xlsx":
		var sheet worksheetData
		sheet, err = readXLSX(content)
		if err == nil {
			cases, err = casesFromRows(sheet.rows)
			fields = sheet.headers
			sheetName = sheet.name
		}
	case ".json", ".jsonl":
		cases, fields, err = casesFromJSON(content)
	default:
		err = fmt.Errorf("unsupported evaluation file %q; use .xlsx, .json, or .jsonl", extension)
	}
	if err != nil {
		return EvaluationAnalysis{}, err
	}
	if len(cases) == 0 {
		return EvaluationAnalysis{}, errors.New("no evaluation cases were found")
	}
	if len(cases) > maxEvaluationRows {
		return EvaluationAnalysis{}, fmt.Errorf(
			"evaluation contains %d cases; the maximum is %d",
			len(cases),
			maxEvaluationRows,
		)
	}

	return buildEvaluationAnalysis(fileName, strings.TrimPrefix(extension, "."), sheetName, fields, cases)
}

func analyzeFoundryEvaluationBundle(
	datasetName string,
	datasetContent []byte,
	sourceName string,
	sourceContent []byte,
	targetName string,
	targetContent []byte,
) (EvaluationAnalysis, error) {
	dataset, err := parseFoundryDataset(datasetContent)
	if err != nil {
		return EvaluationAnalysis{}, fmt.Errorf("parse %q: %w", datasetName, err)
	}
	source, sourceModel, sourceRunID, sourceEvaluationID, err := parseFoundryRun(sourceContent, "source")
	if err != nil {
		return EvaluationAnalysis{}, fmt.Errorf("parse %q: %w", sourceName, err)
	}
	target, targetModel, targetRunID, targetEvaluationID, err := parseFoundryRun(targetContent, "target")
	if err != nil {
		return EvaluationAnalysis{}, fmt.Errorf("parse %q: %w", targetName, err)
	}
	if sourceEvaluationID != "" &&
		targetEvaluationID != "" &&
		sourceEvaluationID != targetEvaluationID {
		return EvaluationAnalysis{}, fmt.Errorf(
			"Source eval_id %q does not match Target eval_id %q",
			sourceEvaluationID,
			targetEvaluationID,
		)
	}
	if sourceRunID != "" && sourceRunID == targetRunID {
		return EvaluationAnalysis{}, fmt.Errorf(
			"Source and Target results report the same run_id %q",
			sourceRunID,
		)
	}
	if len(dataset) != len(source) || len(dataset) != len(target) {
		return EvaluationAnalysis{}, fmt.Errorf(
			"bundle case counts do not match: dataset=%d, Source=%d, Target=%d",
			len(dataset),
			len(source),
			len(target),
		)
	}

	cases := make([]evaluationCase, 0, len(dataset))
	for _, caseID := range sortedFoundryCaseIDs(dataset) {
		datasetItem := dataset[caseID]
		sourceItem, ok := source[caseID]
		if !ok {
			return EvaluationAnalysis{}, fmt.Errorf("Source results are missing case %q", caseID)
		}
		targetItem, ok := target[caseID]
		if !ok {
			return EvaluationAnalysis{}, fmt.Errorf("Target results are missing case %q", caseID)
		}
		if err := validateFoundryDatasetItem(datasetItem, sourceItem.dataset, "Source"); err != nil {
			return EvaluationAnalysis{}, err
		}
		if err := validateFoundryDatasetItem(datasetItem, targetItem.dataset, "Target"); err != nil {
			return EvaluationAnalysis{}, err
		}

		item := evaluationCase{
			caseID:             caseID,
			question:           datasetItem.query,
			sourceOutput:       sourceItem.output,
			targetOutput:       targetItem.output,
			sourceLatency:      sourceItem.latency,
			targetLatency:      targetItem.latency,
			sourceInputTokens:  sourceItem.inputTokens,
			sourceOutputTokens: sourceItem.outputTokens,
			targetInputTokens:  targetItem.inputTokens,
			targetOutputTokens: targetItem.outputTokens,
			assessments:        append(slices.Clone(sourceItem.assessments), targetItem.assessments...),
		}
		_, targetStatus, rationale := qualityAssessment(item)
		if targetStatus == "fail" {
			item.failureKind = inferFailureKind(rationale)
			item.failureDetail = rationale
			item.failureSource = "inferred"
		}
		cases = append(cases, item)
	}

	result, err := buildEvaluationAnalysis(
		"Foundry evaluation bundle",
		"foundry-bundle",
		"",
		[]string{
			"datasource_item.id",
			"datasource_item.query",
			"datasource_item.sample.output_text",
			"results[].passed",
			"results[].score",
			"sample.latency_ms",
			"sample.usage",
			"sample.model",
			"run_id",
			"eval_id",
		},
		cases,
	)
	if err != nil {
		return EvaluationAnalysis{}, err
	}
	result.SourceModel = sourceModel
	result.TargetModel = targetModel
	result.SourceRunID = sourceRunID
	result.TargetRunID = targetRunID
	if sourceEvaluationID != "" {
		result.EvaluationID = sourceEvaluationID
	} else {
		result.EvaluationID = targetEvaluationID
	}
	return result, nil
}

func parseFoundryDataset(content []byte) (map[string]foundryDatasetItem, error) {
	records, err := foundryRecords(content)
	if err != nil {
		return nil, err
	}
	items := make(map[string]foundryDatasetItem, len(records))
	for index, record := range records {
		item, err := foundryDatasetItemFromRecord(record)
		if err != nil {
			return nil, fmt.Errorf("case %d: %w", index+1, err)
		}
		if _, exists := items[item.caseID]; exists {
			return nil, fmt.Errorf("duplicate case ID %q", item.caseID)
		}
		items[item.caseID] = item
	}
	return items, nil
}

func sortedFoundryCaseIDs(items map[string]foundryDatasetItem) []string {
	caseIDs := make([]string, 0, len(items))
	for caseID := range items {
		caseIDs = append(caseIDs, caseID)
	}
	slices.Sort(caseIDs)
	return caseIDs
}

func parseFoundryRun(
	content []byte,
	subject string,
) (map[string]foundryRunItem, string, string, string, error) {
	records, err := foundryRecords(content)
	if err != nil {
		return nil, "", "", "", err
	}
	items := make(map[string]foundryRunItem, len(records))
	var model string
	var runID string
	var evaluationID string
	for index, record := range records {
		if stringAt(record, "object") != "eval.run.output_item" {
			return nil, "", "", "", fmt.Errorf(
				"record %d is not a Foundry eval.run.output_item",
				index+1,
			)
		}
		datasource, ok := valueAt(record, "datasource_item").(map[string]any)
		if !ok {
			return nil, "", "", "", fmt.Errorf("record %d has no datasource_item", index+1)
		}
		datasetItem, err := foundryDatasetItemFromRecord(datasource)
		if err != nil {
			return nil, "", "", "", fmt.Errorf("record %d: %w", index+1, err)
		}
		item := foundryRunItem{
			dataset:      datasetItem,
			output:       stringAt(datasource, "sample.output_text"),
			model:        stringAt(record, "sample", "model"),
			runID:        stringAt(record, "run_id"),
			evaluationID: stringAt(record, "eval_id"),
			latency:      floatAt(record, "sample", "latency_ms"),
			inputTokens:  floatAt(record, "sample", "usage", "prompt_tokens"),
			outputTokens: floatAt(record, "sample", "usage", "completion_tokens"),
		}
		results, ok := valueAt(record, "results").([]any)
		if !ok || len(results) == 0 {
			return nil, "", "", "", fmt.Errorf("record %d has no evaluator results", index+1)
		}
		for _, value := range results {
			result, ok := value.(map[string]any)
			if !ok {
				continue
			}
			status := foundryPassedStatus(result["passed"])
			if status == "" {
				continue
			}
			item.assessments = append(item.assessments, evaluationAssessment{
				evaluator: stringAt(result, "name"),
				subject:   subject,
				status:    status,
				score:     floatAt(result, "score"),
				rationale: foundryEvaluatorRationale(result),
			})
		}
		if len(item.assessments) == 0 {
			return nil, "", "", "", fmt.Errorf(
				"record %d has no completed evaluator results",
				index+1,
			)
		}
		if _, exists := items[datasetItem.caseID]; exists {
			return nil, "", "", "", fmt.Errorf("duplicate case ID %q", datasetItem.caseID)
		}
		if err := preserveFoundryRunMetadata(&model, item.model, "model"); err != nil {
			return nil, "", "", "", err
		}
		if err := preserveFoundryRunMetadata(&runID, item.runID, "run_id"); err != nil {
			return nil, "", "", "", err
		}
		if err := preserveFoundryRunMetadata(
			&evaluationID,
			item.evaluationID,
			"eval_id",
		); err != nil {
			return nil, "", "", "", err
		}
		items[datasetItem.caseID] = item
	}
	if model == "" {
		return nil, "", "", "", errors.New("Foundry run does not report sample.model")
	}
	if runID == "" {
		return nil, "", "", "", errors.New("Foundry run does not report run_id")
	}
	return items, model, runID, evaluationID, nil
}

func foundryRecords(content []byte) ([]map[string]any, error) {
	var document any
	if err := json.Unmarshal(content, &document); err == nil {
		switch value := document.(type) {
		case []any:
			records := make([]map[string]any, 0, len(value))
			for _, item := range value {
				if record, ok := item.(map[string]any); ok {
					records = append(records, record)
				}
			}
			if len(records) > 0 {
				return records, nil
			}
		case map[string]any:
			return []map[string]any{value}, nil
		}
	}

	records := make([]map[string]any, 0)
	for index, line := range bytes.Split(content, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			return nil, fmt.Errorf("line %d is not valid JSON: %w", index+1, err)
		}
		records = append(records, record)
	}
	if len(records) == 0 {
		return nil, errors.New("no records were found")
	}
	return records, nil
}

func foundryDatasetItemFromRecord(record map[string]any) (foundryDatasetItem, error) {
	caseID := strings.TrimSpace(valueString(record["id"]))
	if caseID == "" {
		return foundryDatasetItem{}, errors.New("id is required")
	}
	return foundryDatasetItem{
		caseID:            caseID,
		query:             stringAt(record, "query"),
		description:       stringAt(record, "description"),
		candidateResponse: stringAt(record, "candidate_response"),
	}, nil
}

func validateFoundryDatasetItem(
	expected foundryDatasetItem,
	actual foundryDatasetItem,
	subject string,
) error {
	if expected.caseID != actual.caseID {
		return fmt.Errorf("%s results case ID %q does not match dataset case %q", subject, actual.caseID, expected.caseID)
	}
	for _, field := range []struct {
		name     string
		expected string
		actual   string
	}{
		{name: "query", expected: expected.query, actual: actual.query},
		{name: "description", expected: expected.description, actual: actual.description},
		{name: "candidate_response", expected: expected.candidateResponse, actual: actual.candidateResponse},
	} {
		if field.expected != field.actual {
			return fmt.Errorf("%s results case %q has a mismatched %s", subject, expected.caseID, field.name)
		}
	}
	return nil
}

func preserveFoundryRunMetadata(current *string, value string, field string) error {
	value = strings.TrimSpace(value)
	if *current == "" {
		*current = value
		return nil
	}
	if value != "" && value != *current {
		return fmt.Errorf("Foundry run contains multiple %s values: %q and %q", field, *current, value)
	}
	return nil
}

func foundryPassedStatus(value any) string {
	passed, ok := value.(bool)
	if !ok {
		return ""
	}
	if passed {
		return "pass"
	}
	return "fail"
}

func foundryEvaluatorRationale(result map[string]any) string {
	if reason := strings.TrimSpace(stringAt(result, "reason")); reason != "" {
		return reason
	}
	output, ok := valueAt(result, "sample", "output").([]any)
	if !ok || len(output) == 0 {
		return ""
	}
	message, ok := output[0].(map[string]any)
	if !ok {
		return ""
	}
	content := strings.TrimSpace(stringAt(message, "content"))
	if content == "" {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return content
	}
	if reason := strings.TrimSpace(stringAt(payload, "reason")); reason != "" {
		return reason
	}
	var parts []string
	if steps, ok := payload["steps"].([]any); ok {
		for _, value := range steps {
			step, ok := value.(map[string]any)
			if !ok {
				continue
			}
			if description := strings.TrimSpace(stringAt(step, "description")); description != "" {
				parts = append(parts, description)
			}
			if conclusion := strings.TrimSpace(stringAt(step, "conclusion")); conclusion != "" {
				parts = append(parts, conclusion)
			}
		}
	}
	return strings.Join(parts, " ")
}

func inferFailureKind(rationale string) string {
	normalized := strings.ToLower(rationale)
	switch {
	case strings.Contains(normalized, "unsupported inference"),
		strings.Contains(normalized, "only implies"),
		strings.Contains(normalized, "suggestive wording"):
		return "unsupported_inference"
	case strings.Contains(normalized, "output-format"),
		strings.Contains(normalized, "output format"),
		strings.Contains(normalized, "markdown heading"),
		strings.Contains(normalized, "plain-text"),
		strings.Contains(normalized, "json"):
		return "output_contract"
	case strings.Contains(normalized, "citation"):
		return "presentation_format"
	case strings.Contains(normalized, "equivalent wording"),
		strings.Contains(normalized, "exact-match"):
		return "semantic_equivalence"
	default:
		return "unlocalized_target_failure"
	}
}

func readXLSX(content []byte) (worksheetData, error) {
	if len(content) < 4 || !bytes.Equal(content[:2], []byte("PK")) {
		return worksheetData{}, errors.New(
			"the evaluation workbook is encrypted or is not a valid .xlsx file",
		)
	}
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return worksheetData{}, fmt.Errorf("open evaluation workbook: %w", err)
	}
	var expanded uint64
	files := make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		expanded += file.UncompressedSize64
		if expanded > maxWorkbookExpandedBytes {
			return worksheetData{}, errors.New("the evaluation workbook expands beyond the 64 MB limit")
		}
		files[filepath.ToSlash(file.Name)] = file
	}

	var workbook xlsxWorkbook
	if err := readWorkbookXML(files, "xl/workbook.xml", &workbook); err != nil {
		return worksheetData{}, err
	}
	var relationships xlsxRelationships
	if err := readWorkbookXML(files, "xl/_rels/workbook.xml.rels", &relationships); err != nil {
		return worksheetData{}, err
	}
	targets := make(map[string]string, len(relationships.Relationships))
	for _, relationship := range relationships.Relationships {
		target := strings.TrimPrefix(filepath.ToSlash(relationship.Target), "/")
		if !strings.HasPrefix(target, "xl/") {
			target = "xl/" + target
		}
		targets[relationship.ID] = target
	}
	sharedStrings, err := readSharedStrings(files)
	if err != nil {
		return worksheetData{}, err
	}

	var candidates []worksheetData
	for _, sheet := range workbook.Sheets {
		target := targets[sheet.RelID]
		if target == "" {
			continue
		}
		data, readErr := readWorksheet(files, target, sheet.Name, sharedStrings)
		if readErr != nil {
			return worksheetData{}, readErr
		}
		candidates = append(candidates, data)
	}
	if len(candidates) == 0 {
		return worksheetData{}, errors.New("the evaluation workbook contains no readable worksheets")
	}
	slices.SortStableFunc(candidates, func(a, b worksheetData) int {
		return comparisonSheetScore(b.headers) - comparisonSheetScore(a.headers)
	})
	if comparisonSheetScore(candidates[0].headers) == 0 {
		return worksheetData{}, errors.New(
			"no worksheet contains recognizable Source and Target evaluation columns",
		)
	}
	return candidates[0], nil
}

func readWorkbookXML(files map[string]*zip.File, name string, destination any) error {
	file := files[name]
	if file == nil {
		return fmt.Errorf("evaluation workbook is missing %s", name)
	}
	reader, err := file.Open()
	if err != nil {
		return fmt.Errorf("open %s: %w", name, err)
	}
	defer reader.Close()
	if err := xml.NewDecoder(reader).Decode(destination); err != nil {
		return fmt.Errorf("parse %s: %w", name, err)
	}
	return nil
}

func readSharedStrings(files map[string]*zip.File) ([]string, error) {
	if files["xl/sharedStrings.xml"] == nil {
		return nil, nil
	}
	var document xlsxSharedStrings
	if err := readWorkbookXML(files, "xl/sharedStrings.xml", &document); err != nil {
		return nil, err
	}
	values := make([]string, 0, len(document.Items))
	for _, item := range document.Items {
		var builder strings.Builder
		builder.WriteString(item.Text)
		for _, run := range item.Runs {
			builder.WriteString(run.Text)
		}
		values = append(values, builder.String())
	}
	return values, nil
}

func readWorksheet(
	files map[string]*zip.File,
	name string,
	sheetName string,
	sharedStrings []string,
) (worksheetData, error) {
	var document xlsxWorksheet
	if err := readWorkbookXML(files, name, &document); err != nil {
		return worksheetData{}, err
	}
	if len(document.Rows) == 0 {
		return worksheetData{name: sheetName}, nil
	}
	headers := rowValues(document.Rows[0], sharedStrings)
	rows := make([]map[string]string, 0, len(document.Rows)-1)
	for _, row := range document.Rows[1:] {
		values := rowValues(row, sharedStrings)
		record := make(map[string]string)
		for index, header := range headers {
			if header == "" || index >= len(values) {
				continue
			}
			record[header] = values[index]
		}
		if slices.ContainsFunc(values, func(value string) bool { return strings.TrimSpace(value) != "" }) {
			rows = append(rows, record)
		}
	}
	return worksheetData{name: sheetName, headers: headers, rows: rows}, nil
}

func rowValues(row xlsxRow, sharedStrings []string) []string {
	maxColumn := -1
	columns := make(map[int]string, len(row.Cells))
	for _, cell := range row.Cells {
		column := cellColumn(cell.Reference)
		if column < 0 {
			continue
		}
		maxColumn = max(maxColumn, column)
		value := cell.Value
		switch cell.Type {
		case "s":
			index, err := strconv.Atoi(value)
			if err == nil && index >= 0 && index < len(sharedStrings) {
				value = sharedStrings[index]
			}
		case "inlineStr":
			value = cell.Inline.Text
		case "b":
			value = map[bool]string{true: "true", false: "false"}[value == "1"]
		}
		columns[column] = value
	}
	values := make([]string, maxColumn+1)
	for column, value := range columns {
		values[column] = value
	}
	return values
}

func cellColumn(reference string) int {
	column := 0
	found := false
	for _, char := range reference {
		if char < 'A' || char > 'Z' {
			break
		}
		found = true
		column = column*26 + int(char-'A'+1)
	}
	if !found {
		return -1
	}
	return column - 1
}

func comparisonSheetScore(headers []string) int {
	score := 0
	for _, header := range headers {
		switch normalizeHeader(header) {
		case "caseid", "id":
			score++
		case "sourceoutput", "oldmodelanswer":
			score += 2
		case "targetoutput", "newmodelanswer":
			score += 2
		case "sourceconclusionstatus", "sourcestatus":
			score += 2
		case "targetconclusionstatus", "targetstatus":
			score += 2
		}
	}
	return score
}

func normalizeHeader(value string) string {
	var builder strings.Builder
	for _, char := range strings.ToLower(strings.TrimSpace(value)) {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			builder.WriteRune(char)
		}
	}
	return builder.String()
}

func casesFromRows(rows []map[string]string) ([]evaluationCase, error) {
	cases := make([]evaluationCase, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for index, row := range rows {
		if strings.EqualFold(rowValue(row, "Include"), "no") {
			continue
		}
		caseID := rowValue(row, "Case ID", "case_id", "caseId", "id")
		if caseID == "" {
			return nil, fmt.Errorf("row %d is missing a case ID", index+2)
		}
		if _, exists := seen[caseID]; exists {
			return nil, fmt.Errorf("duplicate case ID %q", caseID)
		}
		seen[caseID] = struct{}{}
		item := evaluationCase{
			caseID:             caseID,
			question:           rowValue(row, "Question", "Input", "Task"),
			sourceOutput:       rowValue(row, "Source Output", "source_output", "Old Model Answer"),
			targetOutput:       rowValue(row, "Target Output", "target_output", "New Model Answer"),
			sourceLatency:      numberValue(rowValue(row, "Source Latency Ms")),
			targetLatency:      numberValue(rowValue(row, "Target Latency Ms")),
			sourceInputTokens:  numberValue(rowValue(row, "Source Input Tokens")),
			sourceOutputTokens: numberValue(rowValue(row, "Source Output Tokens")),
			targetInputTokens:  numberValue(rowValue(row, "Target Input Tokens")),
			targetOutputTokens: numberValue(rowValue(row, "Target Output Tokens")),
			failureKind:        rowValue(row, "Failure Kind"),
			failureDetail:      rowValue(row, "Failure Detail"),
			expectedReasoning:  rowValue(row, "Expected Reasoning"),
			supportingEvidence: splitList(rowValue(row, "Supporting Evidence")),
			failureSource:      rowValue(row, "Failure Source"),
		}
		for _, evaluator := range []struct {
			name         string
			sourceStatus string
			sourceScore  string
			targetStatus string
			targetScore  string
			targetReason string
		}{
			{
				name:         "recall",
				sourceStatus: "Source Recall Status",
				sourceScore:  "Source Recall Score",
				targetStatus: "Target Recall Status",
				targetScore:  "Target Recall Score",
			},
			{
				name:         "conclusion-accuracy",
				sourceStatus: "Source Conclusion Status",
				sourceScore:  "Source Conclusion Score",
				targetStatus: "Target Conclusion Status",
				targetScore:  "Target Conclusion Score",
				targetReason: "Target Conclusion Rationale",
			},
		} {
			sourceStatus := normalizeStatus(rowValue(row, evaluator.sourceStatus))
			targetStatus := normalizeStatus(rowValue(row, evaluator.targetStatus))
			if sourceStatus != "" {
				item.assessments = append(item.assessments, evaluationAssessment{
					evaluator: evaluator.name,
					subject:   "source",
					status:    sourceStatus,
					score:     numberValue(rowValue(row, evaluator.sourceScore)),
				})
			}
			if targetStatus != "" {
				item.assessments = append(item.assessments, evaluationAssessment{
					evaluator: evaluator.name,
					subject:   "target",
					status:    targetStatus,
					score:     numberValue(rowValue(row, evaluator.targetScore)),
					rationale: rowValue(row, evaluator.targetReason),
				})
			}
		}
		if len(item.assessments) == 0 {
			return nil, fmt.Errorf("case %q contains no recognizable evaluator results", caseID)
		}
		cases = append(cases, item)
	}
	return cases, nil
}

func rowValue(row map[string]string, aliases ...string) string {
	for key, value := range row {
		normalized := normalizeHeader(key)
		if slices.ContainsFunc(aliases, func(alias string) bool {
			return normalized == normalizeHeader(alias)
		}) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func numberValue(value string) *float64 {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return nil
	}
	return new(number)
}

func normalizeStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "pass", "passed", "success", "succeeded", "true", "1":
		return "pass"
	case "fail", "failed", "failure", "false", "0":
		return "fail"
	default:
		return ""
	}
}

func splitList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, "|")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func casesFromJSON(content []byte) ([]evaluationCase, []string, error) {
	var records []map[string]any
	var document any
	if err := json.Unmarshal(content, &document); err == nil {
		switch value := document.(type) {
		case []any:
			for _, item := range value {
				if record, ok := item.(map[string]any); ok {
					records = append(records, record)
				}
			}
		case map[string]any:
			for _, key := range []string{"cases", "results", "items", "records", "evaluations"} {
				if values, ok := value[key].([]any); ok {
					for _, item := range values {
						if record, ok := item.(map[string]any); ok {
							records = append(records, record)
						}
					}
					break
				}
			}
			if len(records) == 0 && stringAt(value, "case_id") != "" {
				records = append(records, value)
			}
		}
	} else {
		for index, line := range bytes.Split(content, []byte{'\n'}) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var record map[string]any
			if lineErr := json.Unmarshal(line, &record); lineErr != nil {
				return nil, nil, fmt.Errorf("line %d is not valid JSON: %w", index+1, lineErr)
			}
			records = append(records, record)
		}
	}
	if len(records) == 0 {
		return nil, nil, errors.New("no evaluation cases were found")
	}

	fieldSet := make(map[string]struct{})
	seen := make(map[string]struct{}, len(records))
	cases := make([]evaluationCase, 0, len(records))
	for index, record := range records {
		for key := range record {
			fieldSet[key] = struct{}{}
		}
		item, err := caseFromCanonicalRecord(record)
		if err != nil {
			return nil, nil, fmt.Errorf("case %d: %w", index+1, err)
		}
		if _, exists := seen[item.caseID]; exists {
			return nil, nil, fmt.Errorf("duplicate case ID %q", item.caseID)
		}
		seen[item.caseID] = struct{}{}
		cases = append(cases, item)
	}
	return cases, sortedSetKeys(fieldSet), nil
}

func caseFromCanonicalRecord(record map[string]any) (evaluationCase, error) {
	caseID := stringAt(record, "case_id")
	if caseID == "" {
		caseID = stringAt(record, "caseId")
	}
	if caseID == "" {
		return evaluationCase{}, errors.New("case_id is required")
	}
	item := evaluationCase{
		caseID:             caseID,
		question:           stringAt(record, "input", "task"),
		sourceOutput:       valueString(valueAt(record, "observations", "source", "output")),
		targetOutput:       valueString(valueAt(record, "observations", "target", "output")),
		sourceLatency:      floatAt(record, "observations", "source", "latency_ms"),
		targetLatency:      floatAt(record, "observations", "target", "latency_ms"),
		sourceInputTokens:  floatAt(record, "observations", "source", "input_tokens"),
		sourceOutputTokens: floatAt(record, "observations", "source", "output_tokens"),
		targetInputTokens:  floatAt(record, "observations", "target", "input_tokens"),
		targetOutputTokens: floatAt(record, "observations", "target", "output_tokens"),
		failureKind:        stringAt(record, "failure", "kind"),
		failureDetail:      stringAt(record, "failure", "detail"),
		expectedReasoning:  stringAt(record, "failure", "expected_reasoning"),
		failureSource:      stringAt(record, "failure", "source"),
	}
	if evidence, ok := valueAt(record, "failure", "supporting_evidence").([]any); ok {
		for _, value := range evidence {
			if text, ok := value.(string); ok && text != "" {
				item.supportingEvidence = append(item.supportingEvidence, text)
			}
		}
	}
	if assessments, ok := record["assessments"].([]any); ok {
		for _, value := range assessments {
			assessment, ok := value.(map[string]any)
			if !ok {
				continue
			}
			item.assessments = append(item.assessments, evaluationAssessment{
				evaluator: stringAt(assessment, "evaluator"),
				subject:   stringAt(assessment, "subject"),
				status:    normalizeStatus(stringAt(assessment, "status")),
				score:     floatAt(assessment, "score"),
				rationale: stringAt(assessment, "rationale"),
			})
		}
	}
	if len(item.assessments) == 0 {
		return evaluationCase{}, errors.New("assessments are required")
	}
	return item, nil
}

func valueAt(record map[string]any, path ...string) any {
	var current any = record
	for _, segment := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[segment]
	}
	return current
}

func stringAt(record map[string]any, path ...string) string {
	value, _ := valueAt(record, path...).(string)
	return value
}

func floatAt(record map[string]any, path ...string) *float64 {
	switch value := valueAt(record, path...).(type) {
	case float64:
		return new(value)
	case json.Number:
		if number, err := value.Float64(); err == nil {
			return new(number)
		}
	}
	return nil
}

func valueString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(encoded)
	}
}

func buildEvaluationAnalysis(
	fileName string,
	format string,
	sheetName string,
	fields []string,
	cases []evaluationCase,
) (EvaluationAnalysis, error) {
	result := EvaluationAnalysis{
		FileName:       fileName,
		Format:         format,
		SheetName:      sheetName,
		CaseCount:      len(cases),
		DetectedFields: fields,
		Cases:          []RegressionCase{},
		Patterns:       []RegressionPattern{},
		Evaluators:     summarizeEvaluators(cases),
	}
	patterns := make(map[string]*RegressionPattern)
	for _, item := range cases {
		sourceStatus, targetStatus, rationale := qualityAssessment(item)
		if sourceStatus == "" || targetStatus == "" {
			result.Warnings = append(
				result.Warnings,
				fmt.Sprintf("Case %s has no paired quality assessment.", item.caseID),
			)
			continue
		}
		latencyDelta := percentDelta(item.sourceLatency, item.targetLatency)
		sourceTokens := sumNumbers(item.sourceInputTokens, item.sourceOutputTokens)
		targetTokens := sumNumbers(item.targetInputTokens, item.targetOutputTokens)
		tokenDelta := percentDelta(sourceTokens, targetTokens)

		outcome := "stable"
		failureKind := item.failureKind
		switch {
		case sourceStatus == "pass" && targetStatus == "fail":
			outcome = "regression"
			result.Regressions++
			if failureKind == "" {
				failureKind = "unlocalized_customer_regression"
			}
		case sourceStatus == "fail" && targetStatus == "pass":
			outcome = "improvement"
			result.Improvements++
		case sourceStatus == "fail" && targetStatus == "fail":
			outcome = "pre_existing_failure"
			result.PreExistingFailures++
		case isOperationalRegression(latencyDelta, tokenDelta):
			outcome = "operational_regression"
			failureKind = "operational_regression"
			result.Operational++
		default:
			result.Stable++
		}
		if outcome != "regression" &&
			outcome != "operational_regression" &&
			outcome != "pre_existing_failure" {
			continue
		}
		if failureKind == "" && outcome == "pre_existing_failure" {
			failureKind = "unlocalized_target_failure"
		}

		promptFixable := promptFixability(failureKind)
		regression := RegressionCase{
			CaseID:              item.caseID,
			Question:            item.question,
			Outcome:             outcome,
			SourceStatus:        sourceStatus,
			TargetStatus:        targetStatus,
			SourceOutput:        item.sourceOutput,
			TargetOutput:        item.targetOutput,
			EvaluatorRationale:  rationale,
			FailureKind:         failureKind,
			FailureDetail:       item.failureDetail,
			ExpectedReasoning:   item.expectedReasoning,
			SupportingEvidence:  item.supportingEvidence,
			FailureSource:       item.failureSource,
			PromptFixable:       promptFixable,
			Confidence:          diagnosisConfidence(item.failureSource, item.failureKind),
			LatencyDeltaPercent: latencyDelta,
			TokenDeltaPercent:   tokenDelta,
		}
		result.Cases = append(result.Cases, regression)
		pattern := patterns[failureKind]
		if pattern == nil {
			pattern = &RegressionPattern{
				Code:          failureKind,
				Label:         failureLabel(failureKind),
				PromptFixable: promptFixable,
			}
			patterns[failureKind] = pattern
		}
		pattern.Count++
		pattern.CaseIDs = append(pattern.CaseIDs, item.caseID)
	}
	for _, code := range sortedPatternKeys(patterns) {
		pattern := patterns[code]
		pattern.Prevalence = float64(pattern.Count) / float64(len(cases))
		result.Patterns = append(result.Patterns, *pattern)
	}
	slices.SortFunc(result.Patterns, func(a, b RegressionPattern) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Label, b.Label)
	})
	return result, nil
}

func qualityAssessment(item evaluationCase) (string, string, string) {
	for _, preferred := range []string{"conclusion-accuracy", "quality"} {
		source, target, rationale := pairedAssessment(item.assessments, preferred)
		if source != "" && target != "" {
			return source, target, rationale
		}
	}
	evaluators := make(map[string]struct{})
	for _, assessment := range item.assessments {
		evaluators[assessment.evaluator] = struct{}{}
	}
	for _, evaluator := range sortedSetKeys(evaluators) {
		normalized := strings.ToLower(evaluator)
		if !strings.Contains(normalized, "quality") &&
			!strings.Contains(normalized, "accuracy") {
			continue
		}
		source, target, rationale := pairedAssessment(item.assessments, evaluator)
		if source != "" && target != "" {
			return source, target, rationale
		}
	}
	for _, evaluator := range sortedSetKeys(evaluators) {
		source, target, rationale := pairedAssessment(item.assessments, evaluator)
		if source != "" && target != "" {
			return source, target, rationale
		}
	}
	return "", "", ""
}

func pairedAssessment(assessments []evaluationAssessment, evaluator string) (string, string, string) {
	var source string
	var target string
	var rationale string
	for _, assessment := range assessments {
		if !strings.EqualFold(assessment.evaluator, evaluator) {
			continue
		}
		switch assessment.subject {
		case "source":
			source = assessment.status
		case "target":
			target = assessment.status
			rationale = assessment.rationale
		}
	}
	return source, target, rationale
}

func summarizeEvaluators(cases []evaluationCase) []EvaluatorSummary {
	type accumulator struct {
		sourcePass   int
		targetPass   int
		sourceCount  int
		targetCount  int
		sourceScores []float64
		targetScores []float64
	}
	values := make(map[string]*accumulator)
	for _, item := range cases {
		for _, assessment := range item.assessments {
			current := values[assessment.evaluator]
			if current == nil {
				current = &accumulator{}
				values[assessment.evaluator] = current
			}
			switch assessment.subject {
			case "source":
				current.sourceCount++
				if assessment.status == "pass" {
					current.sourcePass++
				}
				if assessment.score != nil {
					current.sourceScores = append(current.sourceScores, *assessment.score)
				}
			case "target":
				current.targetCount++
				if assessment.status == "pass" {
					current.targetPass++
				}
				if assessment.score != nil {
					current.targetScores = append(current.targetScores, *assessment.score)
				}
			}
		}
	}
	result := make([]EvaluatorSummary, 0, len(values))
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		value := values[name]
		result = append(result, EvaluatorSummary{
			Name:               name,
			SourcePassCount:    value.sourcePass,
			TargetPassCount:    value.targetPass,
			SourcePassRate:     ratio(value.sourcePass, value.sourceCount),
			TargetPassRate:     ratio(value.targetPass, value.targetCount),
			SourceAverageScore: average(value.sourceScores),
			TargetAverageScore: average(value.targetScores),
		})
	}
	return result
}

func sortedPatternKeys(values map[string]*RegressionPattern) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	slices.Sort(result)
	return result
}

func sortedSetKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	slices.Sort(result)
	return result
}

func ratio(numerator int, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func average(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	var total float64
	for _, value := range values {
		total += value
	}
	return new(total / float64(len(values)))
}

func sumNumbers(values ...*float64) *float64 {
	var total float64
	found := false
	for _, value := range values {
		if value != nil {
			total += *value
			found = true
		}
	}
	if !found {
		return nil
	}
	return new(total)
}

func percentDelta(source *float64, target *float64) *float64 {
	if source == nil || target == nil || *source == 0 {
		return nil
	}
	return new((*target - *source) / *source * 100)
}

func isOperationalRegression(latencyDelta *float64, tokenDelta *float64) bool {
	return latencyDelta != nil && *latencyDelta > 20 ||
		tokenDelta != nil && *tokenDelta > 20
}

func promptFixability(kind string) string {
	switch kind {
	case "semantic_equivalence", "cross_context_synthesis", "presentation_format",
		"implied_disclosure", "output_contract", "unsupported_inference":
		return "candidate"
	case "retrieval_context", "operational_regression":
		return "no"
	default:
		return "unknown"
	}
}

func diagnosisConfidence(source string, kind string) string {
	if kind == "operational_regression" {
		return "high"
	}
	switch strings.ToLower(source) {
	case "customer", "evaluator":
		return "high"
	case "inferred":
		return "medium"
	default:
		return "low"
	}
}

func failureLabel(kind string) string {
	labels := map[string]string{
		"semantic_equivalence":            "Semantic equivalence",
		"cross_context_synthesis":         "Cross-context synthesis",
		"presentation_format":             "Presentation format",
		"implied_disclosure":              "Implied disclosure",
		"retrieval_context":               "Retrieval or context",
		"output_contract":                 "Output contract",
		"unsupported_inference":           "Unsupported inference",
		"operational_regression":          "Operational efficiency",
		"unlocalized_customer_regression": "Unlocalized customer regression",
		"unlocalized_target_failure":      "Unlocalized Target failure",
		"other":                           "Other customer regression",
	}
	if label := labels[kind]; label != "" {
		return label
	}
	return strings.ReplaceAll(kind, "_", " ")
}
