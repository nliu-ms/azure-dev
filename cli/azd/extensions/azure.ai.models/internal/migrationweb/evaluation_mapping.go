// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
)

type mappedObservation struct {
	caseID, key, input, output, status, rationale, model, runID string
	inputValue                                                  any
	context, expectedOutput, expectedReferences                 any
	score, latency, inputTokens, outputTokens                   *float64
	recordIndex                                                 int
}

func classifiedMappingStatus(status string) bool {
	return status == "pass" || status == "fail"
}

func mappingIssue(preview *MappingPreview, severity, code, message, caseID string) {
	// Keep counts authoritative while bounding potentially repetitive local diagnostics.
	if len(preview.Issues) < 200 {
		preview.Issues = append(preview.Issues, MappingIssue{
			Severity: severity, Code: code, Message: message, CaseID: caseID,
		})
	}
	if severity == "error" {
		preview.Valid = false
	}
}

func mappingRows(files []mappingFile, file int, collection string) ([]map[string]any, error) {
	if file < 0 || file >= len(files) {
		return nil, errors.New("select an uploaded file for each role")
	}
	rows, ok := files[file].collections[collection]
	if !ok {
		return nil, fmt.Errorf("collection %q does not exist in file %d", collection, file+1)
	}
	return rows, nil
}

func mappingScalar(value any) (string, bool) {
	switch value := value.(type) {
	case nil:
		return "null", true
	case string:
		return value, true
	case json.Number:
		return value.String(), true
	case bool:
		return strconv.FormatBool(value), true
	default:
		return "", false
	}
}

func matchesMappingFilters(value any, filters []MappingFilter) bool {
	for _, filter := range filters {
		actual, exists := pointerValue(value, filter.Path)
		text, scalar := mappingScalar(actual)
		if filter.Path == "" || !exists || !scalar || text != filter.Value {
			return false
		}
	}
	return true
}

func mappingCaseKey(value any) (string, string, error) {
	switch value.(type) {
	case string, json.Number:
		text, _ := mappingScalar(value)
		if text != "" {
			// Type is part of identity: the string "001" is not the number 1.
			return text, mappingValueType(value) + ":" + text, nil
		}
	}
	return "", "", errors.New("case ID must be a nonempty string or number; row-order joins are unsupported")
}

func mappingNumber(value any) (*float64, error) {
	if value == nil || value == "" {
		return nil, nil
	}
	var text string
	switch value := value.(type) {
	case json.Number:
		text = value.String()
	case string:
		text = value
	default:
		return nil, errors.New("expected a finite number or numeric table cell")
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return nil, errors.New("expected a finite number")
	}
	return new(number), nil
}

func mappedNumberAt(value any, path string) (*float64, error) {
	if path == "" {
		return nil, nil
	}
	number, _ := pointerValue(value, path)
	return mappingNumber(number)
}

func mappingReportedStatus(value any) (string, error) {
	if value == nil || value == "" {
		return "unknown", nil
	}
	text, scalar := mappingScalar(value)
	if !scalar {
		return "unknown", errors.New("grade must be a boolean or a supported scalar status")
	}
	if status := normalizeStatus(text); status != "" {
		return status, nil
	}
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "unknown", "pending", "not_evaluated":
		return "unknown", nil
	case "error":
		return "error", nil
	case "skipped", "skip":
		return "skipped", nil
	default:
		return "unknown", fmt.Errorf("unsupported grade value %q; select a supported grade or explicit threshold", text)
	}
}

func selectMappingEvaluator(row map[string]any, mapping MappingEvaluator) (any, error) {
	if mapping.Collection == "" {
		if len(mapping.Filters) != 0 {
			return nil, errors.New("evaluator filters require an evaluator collection")
		}
		return row, nil
	}
	value, _ := pointerValue(row, mapping.Collection)
	values, ok := value.([]any)
	if !ok {
		return nil, errors.New("selected evaluator collection is missing or is not an array")
	}
	var selected any
	count := 0
	for _, value := range values {
		if matchesMappingFilters(value, mapping.Filters) {
			selected = value
			count++
		}
	}
	if count != 1 {
		return nil, fmt.Errorf("evaluator selector must match exactly one item, matched %d", count)
	}
	return selected, nil
}

func validateMappingLane(lane MappingLane, files []mappingFile, dataset bool) error {
	rows, err := mappingRows(files, lane.File, lane.Collection)
	if err != nil {
		return err
	}
	if lane.CaseID == "" || lane.Output == "" || (lane.Input == "" && !dataset) {
		return errors.New("case ID, output, and input (or dataset input) must be explicitly mapped")
	}
	if lane.PassRule != "reported" && lane.PassRule != "threshold" {
		return errors.New("choose a reported status or an explicit threshold rule")
	}
	if lane.Operator != "gte" && lane.Operator != "lte" {
		return errors.New("threshold operator must be gte or lte")
	}
	if lane.PassRule == "reported" && lane.Evaluator.Status == "" {
		return errors.New("reported grading requires an explicit status path")
	}
	if lane.PassRule == "threshold" && (lane.Evaluator.Score == "" || lane.Threshold == nil) {
		return errors.New("score-only grading requires an explicit threshold; no threshold is inferred")
	}
	if lane.Threshold != nil && (math.IsInf(*lane.Threshold, 0) || math.IsNaN(*lane.Threshold)) {
		return errors.New("threshold must be finite")
	}
	paths := []string{
		lane.CaseID, lane.Input, lane.Output, lane.Model, lane.RunID,
		lane.Context, lane.ExpectedOutput, lane.ExpectedReferences,
		lane.LatencyMs, lane.InputTokens, lane.OutputTokens, lane.Evaluator.Collection,
	}
	for _, filter := range lane.Filters {
		if filter.Path == "" {
			return errors.New("partition filters require a field path")
		}
		paths = append(paths, filter.Path)
	}
	for _, path := range paths {
		if path != "" && !slices.ContainsFunc(rows, func(row map[string]any) bool {
			_, exists := pointerValue(row, path)
			return exists
		}) {
			return fmt.Errorf("mapped field %q does not exist", path)
		}
	}
	return nil
}

func mappedLane(
	files []mappingFile, lane MappingLane, role string, dataset bool, expected string, preview *MappingPreview,
) map[string]mappedObservation {
	items := map[string]mappedObservation{}
	if err := validateMappingLane(lane, files, dataset); err != nil {
		mappingIssue(preview, "error", "INVALID_MAPPING", role+": "+err.Error(), "")
		return items
	}
	rows, _ := mappingRows(files, lane.File, lane.Collection)
	selected := 0
	gradePathSeen := false
	evaluatorPaths := map[string]bool{}
	for recordIndex, row := range rows {
		if !matchesMappingFilters(row, lane.Filters) {
			continue
		}
		selected++
		if selected > maxEvaluationRows {
			mappingIssue(preview, "error", "ROW_LIMIT", role+" exceeds the 10,000-record limit", "")
			break
		}
		id, _ := pointerValue(row, lane.CaseID)
		caseID, key, err := mappingCaseKey(id)
		if err != nil {
			mappingIssue(preview, "error", "MISSING_CASE_ID", role+": "+err.Error(), "")
			continue
		}
		if _, exists := items[key]; exists {
			mappingIssue(preview, "error", "DUPLICATE_CASE_KEY", role+" has a duplicate case ID", caseID)
			continue
		}
		item := mappedObservation{caseID: caseID, key: key, status: "unknown", recordIndex: recordIndex}
		item.context, _ = optionalMappingValue(row, lane.Context)
		item.expectedOutput, _ = optionalMappingValue(row, lane.ExpectedOutput)
		item.expectedReferences, _ = optionalMappingValue(row, lane.ExpectedReferences)
		if lane.Input != "" {
			value, exists := pointerValue(row, lane.Input)
			if !exists || value == nil {
				mappingIssue(preview, "error", "MISSING_INPUT", role+" input is missing", caseID)
			}
			item.inputValue, item.input = value, valueString(value)
		}
		output, outputExists := pointerValue(row, lane.Output)
		item.output = valueString(output)
		for _, field := range []struct {
			path string
			dest **float64
		}{{lane.LatencyMs, &item.latency}, {lane.InputTokens, &item.inputTokens}, {lane.OutputTokens, &item.outputTokens}} {
			number, err := mappedNumberAt(row, field.path)
			if err != nil || (number != nil && *number < 0) {
				mappingIssue(preview, "error", "INVALID_METRIC", role+" metric must be nonnegative", caseID)
			}
			*field.dest = number
		}
		for _, field := range []struct {
			path string
			dest *string
		}{{lane.Model, &item.model}, {lane.RunID, &item.runID}} {
			if field.path == "" {
				continue
			}
			value, exists := pointerValue(row, field.path)
			if !exists || value == nil || value == "" {
				mappingIssue(preview, "warning", "UNVERIFIED_IDENTITY", role+" identity is missing on a row", caseID)
			} else if text, ok := value.(string); ok {
				*field.dest = text
			} else {
				mappingIssue(preview, "error", "INVALID_IDENTITY", role+" identity must be a string", caseID)
			}
		}
		// Reported models remain independently binding even if the user leaves the model path empty.
		for _, path := range []string{
			lane.Model, "/model", "/sample/model", "/response/model", "/observations/" + strings.ToLower(role) + "/model",
		} {
			if path == "" {
				continue
			}
			if value, exists := pointerValue(row, path); exists && value != nil && value != "" {
				if model, ok := value.(string); !ok || !modelNamesCompatible(model, expected) {
					mappingIssue(preview, "error", "MODEL_IDENTITY_CONFLICT",
						role+" reported model conflicts with binding", caseID)
				} else if item.model != "" && !modelNamesCompatible(item.model, model) {
					mappingIssue(preview, "error", "MODEL_IDENTITY_CONFLICT",
						role+" contains contradictory reported model fields", caseID)
				} else if item.model == "" {
					item.model = model
				}
			}
		}
		evaluator, err := selectMappingEvaluator(row, lane.Evaluator)
		if err != nil {
			mappingIssue(preview, "error", "AMBIGUOUS_EVALUATOR", role+": "+err.Error(), caseID)
		} else {
			for _, path := range []string{lane.Evaluator.Status, lane.Evaluator.Score, lane.Evaluator.Rationale} {
				if path != "" {
					_, exists := pointerValue(evaluator, path)
					evaluatorPaths[path] = evaluatorPaths[path] || exists
				}
			}
			statusValue, statusExists := pointerValue(evaluator, lane.Evaluator.Status)
			_, scoreExists := pointerValue(evaluator, lane.Evaluator.Score)
			gradePathSeen = gradePathSeen || (lane.PassRule == "reported" && statusExists) ||
				(lane.PassRule == "threshold" && scoreExists)
			item.score, err = mappedNumberAt(evaluator, lane.Evaluator.Score)
			if err != nil {
				mappingIssue(preview, "error", "INVALID_SCORE", role+": "+err.Error(), caseID)
			}
			if lane.Evaluator.Rationale != "" {
				value, _ := pointerValue(evaluator, lane.Evaluator.Rationale)
				item.rationale = valueString(value)
			}
			if lane.PassRule == "reported" {
				item.status, err = mappingReportedStatus(statusValue)
				if err != nil {
					mappingIssue(preview, "error", "INVALID_GRADE", role+": "+err.Error(), caseID)
				}
			} else if item.score != nil {
				passed := *item.score >= *lane.Threshold
				if lane.Operator == "lte" {
					passed = *item.score <= *lane.Threshold
				}
				item.status = "fail"
				if passed {
					item.status = "pass"
				}
			}
			if lane.PassRule == "threshold" && lane.Evaluator.Status != "" && statusExists {
				reported, statusErr := mappingReportedStatus(statusValue)
				if statusErr != nil {
					mappingIssue(preview, "error", "INVALID_GRADE", role+": "+statusErr.Error(), caseID)
				} else if reported == "error" || reported == "skipped" {
					item.status = reported
				} else if classifiedMappingStatus(reported) &&
					classifiedMappingStatus(item.status) && reported != item.status {
					mappingIssue(preview, "error", "GRADE_CONFLICT",
						role+" reported grade contradicts the selected threshold", caseID)
				}
			}
			validatePromptfooMapping(row, lane, evaluator, &item, role, preview)
		}
		if !outputExists || output == nil {
			severity := "error"
			if item.status == "error" || item.status == "skipped" {
				severity = "warning"
			}
			mappingIssue(preview, severity, "MISSING_OUTPUT", role+" output is missing (not an empty answer)", caseID)
		}
		items[key] = item
	}
	if selected == 0 {
		mappingIssue(preview, "error", "EMPTY_PARTITION", role+" partition contains no records", "")
	} else if !gradePathSeen {
		mappingIssue(preview, "error", "MISSING_PASS_RULE", role+" selected grade path never occurs", "")
	}
	for _, path := range slices.Sorted(maps.Keys(evaluatorPaths)) {
		if !evaluatorPaths[path] {
			mappingIssue(preview, "error", "UNKNOWN_EVALUATOR_PATH", role+" evaluator path does not exist: "+path, "")
		}
	}
	return items
}

func validatePromptfooMapping(
	row map[string]any, lane MappingLane, evaluator any, item *mappedObservation, role string, preview *MappingPreview,
) {
	components, hasComponents := pointerValue(row, "/gradingResult/componentResults")
	if !isPromptfooRecord(row) {
		return
	}
	if lane.CaseID == "/id" || lane.CaseID == "/testIdx" || lane.CaseID == "/promptIdx" {
		mappingIssue(preview, "error", "UNSTABLE_CASE_ID",
			"promptfoo observation IDs and export coordinates are not stable case keys", item.caseID)
	}
	if vars, exists := row["vars"]; exists {
		if other, exists := pointerValue(row, "/testCase/vars"); exists && valueString(vars) != valueString(other) {
			mappingIssue(preview, "error", "INPUT_CONFLICT", role+" vars conflicts with testCase.vars", item.caseID)
		}
	}
	if strings.HasPrefix(lane.Model, "/provider") {
		mappingIssue(preview, "error", "PROVIDER_NOT_MODEL", "provider identity is not reported model identity", item.caseID)
	}
	if strings.HasPrefix(lane.InputTokens, "/tokenUsage") || strings.HasPrefix(lane.OutputTokens, "/tokenUsage") {
		mappingIssue(preview, "error", "MIXED_TOKEN_USAGE", "use response.tokenUsage for answer-model tokens", item.caseID)
	}
	if lane.Evaluator.Status == "/success" || (hasComponents && lane.Evaluator.Collection == "" &&
		(lane.Evaluator.Status == "/gradingResult/pass" || lane.Evaluator.Score == "/gradingResult/score")) {
		if values, ok := components.([]any); ok && len(values) > 0 {
			mappingIssue(preview, "error", "MIXED_AGGREGATE",
				"select an individual quality assertion rather than the overall test outcome", item.caseID)
		}
	}
	if lane.Evaluator.Collection == "" {
		selectedIndex := ""
		for _, path := range []string{lane.Evaluator.Status, lane.Evaluator.Score, lane.Evaluator.Rationale} {
			const prefix = "/gradingResult/componentResults/"
			if strings.HasPrefix(path, prefix) {
				index, _, _ := strings.Cut(strings.TrimPrefix(path, prefix), "/")
				if selectedIndex != "" && selectedIndex != index {
					mappingIssue(preview, "error", "AMBIGUOUS_EVALUATOR",
						"grade fields refer to different promptfoo assertions", item.caseID)
				}
				selectedIndex = index
				evaluator, _ = pointerValue(row, prefix+index)
				mappingIssue(preview, "warning", "POSITIONAL_EVALUATOR",
					"indexed evaluator selection requires stable assertion order; prefer a unique selector", item.caseID)
			}
		}
	}
	if children, exists := pointerValue(evaluator, "/componentResults"); exists {
		if values, ok := children.([]any); ok && len(values) > 0 {
			mappingIssue(preview, "error", "COMPOSITE_EVALUATOR",
				"select one quality assertion; composite assertion semantics are not inferred", item.caseID)
		}
	}
	assertion, _ := pointerValue(evaluator, "/assertion/type")
	switch assertion {
	case "latency", "cost", "perplexity":
		mappingIssue(preview, "error", "NOT_QUALITY", "selected assertion is operational, not quality", item.caseID)
	}
	if reason, ok := mappingScalar(row["failureReason"]); ok && reason == "2" {
		item.status = "error"
	}
	if value, exists := pointerValue(row, "/response/error"); exists && value != nil && value != "" {
		item.status = "error"
	}
	// ASSERT (1) may copy an assertion reason into error; it is not an execution failure.
}

func isPromptfooRecord(row map[string]any) bool {
	_, grading := row["gradingResult"].(map[string]any)
	_, response := row["response"].(map[string]any)
	_, provider := row["provider"]
	_, components := pointerValue(row, "/gradingResult/componentResults")
	return grading && response && (provider || components)
}

func mappingDigest(files []mappingFile, plan EvaluationMapping, sourceModel, targetModel string) string {
	hashes := make([]string, len(files))
	for index, file := range files {
		hashes[index] = file.profile.SHA256 + ":" + file.profile.Format
	}
	body, _ := json.Marshal(struct {
		Version int
		Files   []string
		Mapping EvaluationMapping
		Source  string
		Target  string
	}{1, hashes, plan, sourceModel, targetModel})
	digest := sha256.Sum256(body)
	return fmt.Sprintf("%x", digest)
}

func previewMapping(
	files []mappingFile, plan EvaluationMapping, sourceModel, targetModel string,
) (MappingPreview, EvaluationAnalysis) {
	preview, analysis := previewMappingRecords(files, plan, sourceModel, targetModel)
	applyMappingRunMetadata(files, plan, sourceModel, targetModel, &preview, &analysis)
	normalizeMappingPreview(&preview)
	return preview, analysis
}

func previewMappingRecords(
	files []mappingFile, plan EvaluationMapping, sourceModel, targetModel string,
) (MappingPreview, EvaluationAnalysis) {
	preview := MappingPreview{
		Valid: true, Issues: []MappingIssue{}, Cases: []MappingPreviewCase{},
		EvaluationSHA256: mappingDigest(files, plan, sourceModel, targetModel),
	}
	if plan.Version != 1 || !slices.Contains([]string{"generic", "canonical", "meera", "foundry"}, plan.Adapter) {
		mappingIssue(&preview, "error", "INVALID_MAPPING", "unsupported mapping version or adapter", "")
		return preview, EvaluationAnalysis{}
	}
	if plan.Adapter != "generic" {
		return previewNativeMapping(files, plan, sourceModel, targetModel, preview)
	}
	source := mappedLane(files, plan.Source, "Source", plan.Dataset != nil, sourceModel, &preview)
	target := mappedLane(files, plan.Target, "Target", plan.Dataset != nil, targetModel, &preview)
	if plan.Source.File == plan.Target.File && plan.Source.Collection == plan.Target.Collection &&
		valueString(plan.Source.Filters) == valueString(plan.Target.Filters) && plan.Source.Output == plan.Target.Output {
		mappingIssue(&preview, "error", "SAME_OBSERVATION", "Source and Target select the same observation", "")
	}
	dataset := mappedDataset(files, plan.Dataset, &preview)
	keys := map[string]bool{}
	for key := range source {
		keys[key] = true
	}
	for key := range target {
		keys[key] = true
	}
	preview.CaseCount = len(keys)
	if preview.CaseCount > maxEvaluationRows {
		mappingIssue(&preview, "error", "ROW_LIMIT", "comparison exceeds the 10,000-case limit", "")
	}
	cases := make([]evaluationCase, 0, len(keys))
	for _, key := range slices.Sorted(maps.Keys(keys)) {
		s, hasSource := source[key]
		t, hasTarget := target[key]
		id := s.caseID
		if !hasSource {
			id = t.caseID
			preview.TargetOnly++
		}
		if !hasTarget {
			preview.SourceOnly++
		}
		if !hasSource || !hasTarget {
			mappingIssue(&preview, "error", "UNMATCHED_CASE", "case is present in only one selected run", id)
		}
		if hasSource && hasTarget && plan.Source.Input != "" && plan.Target.Input != "" &&
			valueStringJSON(s.inputValue) != valueStringJSON(t.inputValue) {
			mappingIssue(&preview, "error", "INPUT_CONFLICT", "Source and Target inputs differ", id)
		}
		if hasSource && hasTarget && plan.Source.File == plan.Target.File &&
			plan.Source.Collection == plan.Target.Collection && plan.Source.Output == plan.Target.Output &&
			s.recordIndex == t.recordIndex {
			mappingIssue(&preview, "error", "SAME_OBSERVATION", "Source and Target select the same observation", id)
		}
		input := s.input
		if plan.Source.Input == "" {
			input = t.input
		}
		if plan.Dataset != nil {
			d, exists := dataset[key]
			if !exists {
				mappingIssue(&preview, "error", "UNMATCHED_DATASET", "dataset is missing case", id)
			} else {
				for _, observation := range []mappedObservation{s, t} {
					if observation.inputValue != nil &&
						valueStringJSON(observation.inputValue) != valueStringJSON(d.inputValue) {
						mappingIssue(&preview, "error", "INPUT_CONFLICT", "run input conflicts with dataset", id)
					}
				}
				input = d.input
			}
		}
		preserveMappedIdentity(&preview.SourceModel, s.model, "Source model", id, &preview)
		preserveMappedIdentity(&preview.TargetModel, t.model, "Target model", id, &preview)
		preserveMappedIdentity(&preview.SourceRunID, s.runID, "Source run", id, &preview)
		preserveMappedIdentity(&preview.TargetRunID, t.runID, "Target run", id, &preview)
		if hasSource && hasTarget && classifiedMappingStatus(s.status) && classifiedMappingStatus(t.status) {
			preview.ComparableCount++
		} else {
			preview.UnclassifiedCount++
			mappingIssue(&preview, "warning", "UNKNOWN_GRADE", "case is excluded from comparable grade denominators", id)
		}
		item := evaluationCase{
			caseID: id, question: input, sourceOutput: s.output, targetOutput: t.output,
			sourceContext: s.context, targetContext: t.context,
			expectedOutput: mergeExpectedEvidence(&preview, id, "expected output",
				s.expectedOutput, t.expectedOutput, dataset[key].expectedOutput),
			expectedReferences: mergeExpectedEvidence(&preview, id, "expected references",
				s.expectedReferences, t.expectedReferences, dataset[key].expectedReferences),
			sourceLatency: s.latency, targetLatency: t.latency,
			sourceInputTokens: s.inputTokens, sourceOutputTokens: s.outputTokens,
			targetInputTokens: t.inputTokens, targetOutputTokens: t.outputTokens,
			selectedQuality: new([3]string{s.status, t.status, t.rationale}),
			assessments: []evaluationAssessment{
				{evaluator: "reviewed-quality", subject: "source", status: s.status, score: s.score, rationale: s.rationale},
				{evaluator: "reviewed-quality", subject: "target", status: t.status, score: t.score, rationale: t.rationale},
			},
		}
		item.selectedAssessments = new([2]evaluationAssessment{item.assessments[0], item.assessments[1]})
		cases = append(cases, item)
		appendMappingPreviewCase(&preview, item, s.score, t.score)
	}
	for _, key := range slices.Sorted(maps.Keys(dataset)) {
		item := dataset[key]
		if !keys[key] {
			mappingIssue(&preview, "error", "UNMATCHED_DATASET",
				"dataset case has no selected run observations", item.caseID)
		}
	}
	if preview.ComparableCount == 0 {
		mappingIssue(&preview, "error", "NO_COMPARABLE_CASES", "at least one comparable pass/fail pair is required", "")
	}
	if preview.SourceModel == "" || preview.TargetModel == "" {
		mappingIssue(&preview, "warning", "UNVERIFIED_MODEL",
			"model identity is unreported; confirmation attests the selected model bindings", "")
	}
	analysis, _ := buildEvaluationAnalysis("Reviewed evaluation mapping", "mapped", "", []string{}, cases)
	analysis.SourceModel, analysis.TargetModel = preview.SourceModel, preview.TargetModel
	analysis.SourceRunID, analysis.TargetRunID = preview.SourceRunID, preview.TargetRunID
	analysis.EvaluationSHA256 = preview.EvaluationSHA256
	appendMappingProvenance(files, &preview, &analysis)
	for _, issue := range preview.Issues {
		if issue.Severity == "warning" && !slices.Contains(analysis.Warnings, issue.Message) {
			analysis.Warnings = append(analysis.Warnings, issue.Message)
		}
	}
	return preview, analysis
}

func valueStringJSON(value any) string {
	content, _ := json.Marshal(value)
	return string(content)
}

func mappedDataset(
	files []mappingFile, dataset *MappingDataset, preview *MappingPreview,
) map[string]mappedObservation {
	items := map[string]mappedObservation{}
	if dataset == nil {
		return items
	}
	rows, err := mappingRows(files, dataset.File, dataset.Collection)
	if err != nil || dataset.CaseID == "" || dataset.Input == "" {
		mappingIssue(preview, "error", "INVALID_DATASET", "dataset requires a collection, stable case ID and input", "")
		return items
	}
	if len(rows) > maxEvaluationRows {
		mappingIssue(preview, "error", "ROW_LIMIT", "dataset exceeds the 10,000-record limit", "")
		return items
	}
	for _, row := range rows {
		id, _ := pointerValue(row, dataset.CaseID)
		caseID, key, err := mappingCaseKey(id)
		if err != nil {
			mappingIssue(preview, "error", "MISSING_CASE_ID", "dataset: "+err.Error(), "")
			continue
		}
		if _, exists := items[key]; exists {
			mappingIssue(preview, "error", "DUPLICATE_CASE_KEY", "dataset contains a duplicate case ID", caseID)
		}
		input, exists := pointerValue(row, dataset.Input)
		if !exists || input == nil {
			mappingIssue(preview, "error", "MISSING_INPUT", "dataset input is missing", caseID)
		}
		reference, referenceExists := optionalMappingValue(row, dataset.Reference)
		expectedOutput, expectedExists := optionalMappingValue(row, dataset.ExpectedOutput)
		if dataset.Reference != "" {
			if !referenceExists {
				mappingIssue(preview, "error", "MISSING_REFERENCE", "mapped dataset reference is missing", caseID)
			}
		}
		if dataset.ExpectedOutput != "" && !expectedExists {
			mappingIssue(preview, "error", "MISSING_REFERENCE", "mapped dataset expected output is missing", caseID)
		}
		items[key] = mappedObservation{
			caseID: caseID, inputValue: input, input: valueString(input),
			expectedOutput: expectedOutput, expectedReferences: reference,
		}
	}

	return items
}

func optionalMappingValue(row any, path string) (any, bool) {
	if path == "" {
		return nil, false
	}
	return pointerValue(row, path)
}

func mergeExpectedEvidence(preview *MappingPreview, caseID, label string, values ...any) any {
	var result any
	for _, value := range values {
		if value == nil {
			continue
		}
		if result != nil && valueStringJSON(result) != valueStringJSON(value) {
			mappingIssue(preview, "error", "REFERENCE_CONFLICT",
				"independently supplied "+label+" conflicts across Source, Target or dataset", caseID)
		} else {
			result = value
		}
	}
	return result
}
func preserveMappedIdentity(dest *string, value, label, caseID string, preview *MappingPreview) {
	if value == "" {
		return
	}
	if *dest != "" && *dest != value {
		mappingIssue(preview, "error", "AMBIGUOUS_PARTITION", label+" varies within the selected partition", caseID)
	}
	*dest = value
}

func appendMappingPreviewCase(preview *MappingPreview, item evaluationCase, sourceScore, targetScore *float64) {
	if len(preview.Cases) >= 10 {
		return
	}
	source, target, _ := qualityAssessment(item)
	preview.Cases = append(preview.Cases, MappingPreviewCase{
		CaseID: item.caseID, Input: localMappingSample(item.question, 2000),
		SourceOutput: localMappingSample(item.sourceOutput, 2000),
		TargetOutput: localMappingSample(item.targetOutput, 2000),
		SourceStatus: source, TargetStatus: target, SourceScore: sourceScore, TargetScore: targetScore,
	})
}
