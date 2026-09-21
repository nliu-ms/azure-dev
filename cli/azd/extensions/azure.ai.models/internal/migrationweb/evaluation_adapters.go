// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

func emptyMappingLane() MappingLane {
	return MappingLane{
		File: -1, Filters: []MappingFilter{}, PassRule: "reported", Operator: "gte",
		Evaluator: MappingEvaluator{Filters: []MappingFilter{}},
	}
}

func suggestMapping(files []mappingFile, sourceModel, targetModel string) (EvaluationMapping, []string) {
	plan := EvaluationMapping{Version: 1, Adapter: "generic", Source: emptyMappingLane(), Target: emptyMappingLane()}
	warnings := []string{
		"Review role bindings, stable case IDs and equivalent quality criteria before confirming.",
		"Mapping confirmation acknowledges unclassified cases and any unreported model identity.",
	}
	for index, file := range files {
		for _, collection := range file.profile.Collections {
			rows := file.collections[collection.Path]
			if len(rows) == 0 {
				continue
			}
			if len(files) == 1 && canonicalMappingShape(rows) {
				plan.Adapter = "canonical"
				plan.Source = nativeCanonicalLane(index, collection.Path, rows, "source")
				plan.Target = nativeCanonicalLane(index, collection.Path, rows, "target")
				return plan, warnings
			}
			if len(files) == 1 && file.profile.Format == "xlsx" && meeraMappingShape(rows) {
				plan.Adapter = "meera"
				plan.Source = nativeMeeraLane(index, collection.Path, rows[0], "Source")
				plan.Target = nativeMeeraLane(index, collection.Path, rows[0], "Target")
				return plan, warnings
			}
		}
	}
	if len(files) == 3 {
		native := plan
		for index, file := range files {
			for _, collection := range file.profile.Collections {
				rows := file.collections[collection.Path]
				if len(rows) == 0 {
					continue
				}
				if stringAt(rows[0], "object") == "eval.run.output_item" {
					model := stringAt(rows[0], "sample", "model")
					if model != "" && sourceModel != "" && modelNamesCompatible(model, sourceModel) {
						native.Source = nativeFoundryLane(index, collection.Path, rows)
					}
					if model != "" && targetModel != "" && modelNamesCompatible(model, targetModel) {
						native.Target = nativeFoundryLane(index, collection.Path, rows)
					}
				} else if _, err := parseFoundryDataset(file.content); err == nil {
					native.Dataset = &MappingDataset{
						File: index, Collection: collection.Path, CaseID: "/id", Input: "/query",
						ExpectedOutput: suggestedPath(rows, "/candidate_response"),
					}
				}
			}
		}
		if native.Dataset != nil && native.Source.File >= 0 && native.Target.File >= 0 &&
			native.Source.File != native.Target.File {
			native.Adapter = "foundry"
			return native, warnings
		}
	}
	for role, expected := range map[string]string{"source": sourceModel, "target": targetModel} {
		candidates := []MappingLane{}
		for index, file := range files {
			for _, collection := range file.profile.Collections {
				rows := file.collections[collection.Path]
				if len(rows) == 0 {
					continue
				}
				lane := suggestGenericLane(index, collection.Path, rows, role)
				if len(files) == 1 && (len(file.collections) == 1 || collection.Path == "/results/results") {
					candidates = append(candidates, lane)
					continue
				}
				if lane.Model != "" && expected != "" {
					for _, row := range rows {
						value, _ := pointerValue(row, lane.Model)
						if model, ok := value.(string); ok && model != "" && modelNamesCompatible(model, expected) {
							lane.Filters = []MappingFilter{{Path: lane.Model, Value: model}}
							candidates = append(candidates, lane)
							break
						}
					}
				}
			}
		}
		if len(candidates) == 1 {
			lane := candidates[0]
			if lane.Model != "" && expected != "" {
				rows := files[lane.File].collections[lane.Collection]
				for _, row := range rows {
					value, _ := pointerValue(row, lane.Model)
					if model, ok := value.(string); ok && model != "" && modelNamesCompatible(model, expected) {
						lane.Filters = []MappingFilter{{Path: lane.Model, Value: model}}
						break
					}
				}
			}
			if role == "source" {
				plan.Source = lane
			} else {
				plan.Target = lane
			}
		}
	}
	warnings = append(warnings,
		"Unknown roles and quality policies remain unresolved; filenames never establish model identity.")
	return plan, warnings
}

func suggestedPath(rows []map[string]any, candidates ...string) string {
	for _, candidate := range candidates {
		if slices.ContainsFunc(rows, func(row map[string]any) bool {
			_, exists := pointerValue(row, candidate)
			return exists
		}) {
			return candidate
		}
	}
	return ""
}

func suggestGenericLane(index int, collection string, rows []map[string]any, role string) MappingLane {
	lane := emptyMappingLane()
	lane.File, lane.Collection = index, collection
	lane.CaseID = suggestedPath(rows, "/case_id", "/caseId", "/vars/case_id", "/testCase/vars/case_id", "/id")
	if collection == "/results/results" || slices.ContainsFunc(rows, isPromptfooRecord) {
		// promptfoo's id and testIdx are observation/export coordinates, not cross-run case IDs.
		lane.CaseID = suggestedPath(rows, "/vars/case_id", "/testCase/vars/case_id", "/case_id")
	}
	lane.Input = suggestedPath(rows, "/input", "/question", "/query", "/vars", "/testCase/vars")
	lane.Context = suggestedPath(rows, "/"+role+"_context", "/context")
	lane.ExpectedOutput = suggestedPath(rows, "/expected_output", "/expected_conclusion")
	lane.ExpectedReferences = suggestedPath(rows, "/expected_references")
	lane.Output = suggestedPath(rows, "/"+role+"_output", "/"+role+"Output", "/response/output", "/output", "/answer")
	lane.Model = suggestedPath(rows, "/model", "/sample/model", "/response/model")
	lane.RunID = suggestedPath(rows, "/run_id", "/runId")
	lane.LatencyMs = suggestedPath(rows, "/latencyMs", "/latency_ms", "/sample/latency_ms")
	lane.InputTokens = suggestedPath(rows, "/response/tokenUsage/prompt", "/input_tokens", "/sample/usage/prompt_tokens")
	lane.OutputTokens = suggestedPath(rows,
		"/response/tokenUsage/completion", "/output_tokens", "/sample/usage/completion_tokens")
	lane.Evaluator.Status = suggestedPath(rows, "/"+role+"_pass", "/"+role+"Pass", "/passed", "/pass", "/status")
	lane.Evaluator.Score = suggestedPath(rows, "/"+role+"_score", "/score")
	lane.Evaluator.Rationale = suggestedPath(rows, "/"+role+"_reason", "/reason", "/rationale")
	if suggestedPath(rows, "/gradingResult/componentResults") != "" {
		lane.Evaluator = MappingEvaluator{
			Collection: "/gradingResult/componentResults", Filters: []MappingFilter{},
			Status: "/pass", Score: "/score", Rationale: "/reason",
		}
		// An empty selector intentionally stays ambiguous when multiple assertions exist.
	}
	return lane
}

func canonicalMappingShape(rows []map[string]any) bool {
	return slices.ContainsFunc(rows, func(row map[string]any) bool {
		_, source := pointerValue(row, "/observations/source/output")
		_, target := pointerValue(row, "/observations/target/output")
		_, assessments := row["assessments"].([]any)
		return source && target && assessments && (stringAt(row, "case_id") != "" || stringAt(row, "caseId") != "")
	})
}

func meeraMappingShape(rows []map[string]any) bool {
	row := rows[0]
	for _, field := range []string{
		"Case ID", "Source Output", "Target Output", "Source Conclusion Status", "Target Conclusion Status",
	} {
		if _, ok := row[field]; !ok {
			return false
		}
	}
	return true
}

func nativeCanonicalLane(index int, collection string, rows []map[string]any, role string) MappingLane {
	lane := emptyMappingLane()
	lane.File, lane.Collection = index, collection
	lane.CaseID, lane.Input = suggestedPath(rows, "/case_id", "/caseId"), "/input/task"
	base := "/observations/" + role
	lane.Context = suggestedPath(rows, base+"/retrieved_context")
	lane.ExpectedOutput = suggestedPath(rows, "/references/expected_output")
	lane.ExpectedReferences = suggestedPath(rows, "/references/expected_evidence")
	lane.Output, lane.LatencyMs = base+"/output", base+"/latency_ms"
	lane.Model = suggestedPath(rows, base+"/model")
	lane.RunID = suggestedPath(rows, base+"/run_id")
	lane.InputTokens, lane.OutputTokens = base+"/input_tokens", base+"/output_tokens"
	var combined evaluationCase
	for _, row := range rows {
		if item, err := caseFromCanonicalRecord(row); err == nil {
			combined.assessments = append(combined.assessments, item.assessments...)
		}
	}
	evaluator := ""
	if names := nativeEvaluatorNames(combined); len(names) > 0 {
		evaluator = names[0]
	}
	lane.Evaluator = MappingEvaluator{
		Collection: "/assessments",
		Filters: []MappingFilter{
			{Path: "/subject", Value: role}, {Path: "/evaluator", Value: evaluator},
		},
		Status: "/status", Score: "/score", Rationale: "/rationale",
	}
	var evaluatorRows []map[string]any
	for _, row := range rows {
		if selected, err := selectMappingEvaluator(row, lane.Evaluator); err == nil {
			if record, ok := selected.(map[string]any); ok {
				evaluatorRows = append(evaluatorRows, record)
			}
		}
	}
	lane.Evaluator.Score = suggestedPath(evaluatorRows, "/score")
	lane.Evaluator.Rationale = suggestedPath(evaluatorRows, "/rationale")
	clearAbsentLanePaths(&lane, rows)
	return lane
}

func nativeEvaluatorNames(item evaluationCase) []string {
	names := map[string]bool{}
	for _, assessment := range item.assessments {
		names[assessment.evaluator] = true
	}
	result := []string{}
	for _, preferred := range []string{"conclusion-accuracy", "quality"} {
		if names[preferred] {
			result = append(result, preferred)
			delete(names, preferred)
		}
		for _, name := range slices.Sorted(maps.Keys(names)) {
			lower := strings.ToLower(name)
			if strings.Contains(lower, "quality") || strings.Contains(lower, "accuracy") {
				result = append(result, name)
				delete(names, name)
			}
		}
	}
	return append(result, slices.Sorted(maps.Keys(names))...)
}

func nativeMeeraLane(index int, collection string, row map[string]any, role string) MappingLane {
	lane := emptyMappingLane()
	lane.File, lane.Collection = index, collection
	lane.CaseID, lane.Input, lane.Output = "/Case ID", "/Question", "/"+role+" Output"
	lane.LatencyMs = "/" + role + " Latency Ms"
	lane.InputTokens, lane.OutputTokens = "/"+role+" Input Tokens", "/"+role+" Output Tokens"
	lane.Evaluator.Status = "/" + role + " Conclusion Status"
	lane.Evaluator.Score = suggestedPath([]map[string]any{row}, "/"+role+" Conclusion Score")
	lane.Evaluator.Rationale = suggestedPath([]map[string]any{row}, "/"+role+" Conclusion Rationale")
	clearAbsentLanePaths(&lane, []map[string]any{row})
	return lane
}

func clearAbsentLanePaths(lane *MappingLane, rows []map[string]any) {
	for _, field := range []*string{&lane.LatencyMs, &lane.InputTokens, &lane.OutputTokens} {
		*field = suggestedPath(rows, *field)
	}
}

func nativeFoundryLane(index int, collection string, rows []map[string]any) MappingLane {
	lane := emptyMappingLane()
	lane.File, lane.Collection = index, collection
	lane.CaseID, lane.Input = "/datasource_item/id", "/datasource_item/query"
	lane.Output, lane.Model, lane.RunID = "/datasource_item/sample.output_text", "/sample/model", "/run_id"
	lane.LatencyMs = "/sample/latency_ms"
	lane.InputTokens, lane.OutputTokens = "/sample/usage/prompt_tokens", "/sample/usage/completion_tokens"
	name := ""
	var item evaluationCase
	for _, row := range rows {
		if results, ok := row["results"].([]any); ok {
			for _, value := range results {
				if result, ok := value.(map[string]any); ok && stringAt(result, "name") != "" {
					item.assessments = append(item.assessments, evaluationAssessment{evaluator: stringAt(result, "name")})
				}
			}
		}
	}
	if names := nativeEvaluatorNames(item); len(names) > 0 {
		name = names[0]
	}
	lane.Evaluator = MappingEvaluator{
		Collection: "/results", Filters: []MappingFilter{{Path: "/name", Value: name}},
		Status: "/passed", Score: "/score", Rationale: "/sample/output",
	}
	clearAbsentLanePaths(&lane, rows)
	return lane
}

func nativePlanForSelection(files []mappingFile, plan EvaluationMapping) (EvaluationMapping, error) {
	expected := plan
	sourceRows, err := mappingRows(files, plan.Source.File, plan.Source.Collection)
	if err != nil || len(sourceRows) == 0 {
		return expected, errors.New("native Source collection is missing")
	}
	targetRows, err := mappingRows(files, plan.Target.File, plan.Target.Collection)
	if err != nil || len(targetRows) == 0 {
		return expected, errors.New("native Target collection is missing")
	}
	switch plan.Adapter {
	case "canonical", "meera":
		if len(files) != 1 || plan.Source.File != plan.Target.File ||
			plan.Source.Collection != plan.Target.Collection || plan.Dataset != nil {
			return expected, errors.New("paired native adapters require one shared collection")
		}
		if plan.Adapter == "canonical" {
			if !canonicalMappingShape(sourceRows) {
				return expected, errors.New("collection does not match canonical evidence")
			}
			expected.Source = nativeCanonicalLane(plan.Source.File, plan.Source.Collection, sourceRows, "source")
			expected.Target = nativeCanonicalLane(plan.Target.File, plan.Target.Collection, targetRows, "target")
		} else {
			if !meeraMappingShape(sourceRows) {
				return expected, errors.New("collection does not match the Meera workbook")
			}
			expected.Source = nativeMeeraLane(plan.Source.File, plan.Source.Collection, sourceRows[0], "Source")
			expected.Target = nativeMeeraLane(plan.Target.File, plan.Target.Collection, targetRows[0], "Target")
		}
	case "foundry":
		if len(files) != 3 || plan.Dataset == nil || plan.Source.File == plan.Target.File ||
			plan.Source.File == plan.Dataset.File || plan.Target.File == plan.Dataset.File {
			return expected, errors.New("native Foundry requires three distinct dataset, Source and Target files")
		}
		if _, err := mappingRows(files, plan.Dataset.File, plan.Dataset.Collection); err != nil {
			return expected, err
		}
		expected.Source = nativeFoundryLane(plan.Source.File, plan.Source.Collection, sourceRows)
		expected.Target = nativeFoundryLane(plan.Target.File, plan.Target.Collection, targetRows)
		expected.Dataset = &MappingDataset{
			File: plan.Dataset.File, Collection: plan.Dataset.Collection, CaseID: "/id", Input: "/query",
			ExpectedOutput: suggestedPath(
				files[plan.Dataset.File].collections[plan.Dataset.Collection], "/candidate_response"),
		}
	}
	return expected, nil
}

func previewNativeMapping(
	files []mappingFile, plan EvaluationMapping, sourceModel, targetModel string, preview MappingPreview,
) (MappingPreview, EvaluationAnalysis) {
	expected, err := nativePlanForSelection(files, plan)
	comparable := plan
	// Optional evidence fields were absent in older native maps; the native reader still preserves their data.
	for _, pair := range [][2]*MappingLane{{&comparable.Source, &expected.Source}, {&comparable.Target, &expected.Target}} {
		for _, fields := range [][2]*string{
			{&pair[0].Context, &pair[1].Context},
			{&pair[0].ExpectedOutput, &pair[1].ExpectedOutput},
			{&pair[0].ExpectedReferences, &pair[1].ExpectedReferences},
		} {
			if *fields[0] == "" {
				*fields[0] = *fields[1]
			}
		}
	}
	if comparable.Dataset != nil && expected.Dataset != nil && comparable.Dataset.ExpectedOutput == "" {
		dataset := *comparable.Dataset
		dataset.ExpectedOutput = expected.Dataset.ExpectedOutput
		comparable.Dataset = &dataset
	}
	if err == nil && valueStringJSON(expected) != valueStringJSON(comparable) {
		err = errors.New("native adapters have fixed semantics; switch to generic to apply field or policy edits")
	}
	if err != nil {
		mappingIssue(&preview, "error", "NATIVE_MAPPING_EDIT", err.Error(), "")
		return preview, EvaluationAnalysis{}
	}
	var analysis EvaluationAnalysis
	var cases []evaluationCase
	if plan.Adapter == "foundry" {
		dataset, source, target := files[plan.Dataset.File], files[plan.Source.File], files[plan.Target.File]
		analysis, err = analyzeFoundryEvaluationBundle(
			dataset.profile.Name, dataset.content, source.profile.Name, source.content, target.profile.Name, target.content,
		)
		if err == nil {
			var datasetItems map[string]foundryDatasetItem
			var sourceItems, targetItems map[string]foundryRunItem
			datasetItems, err = parseFoundryDataset(dataset.content)
			if err == nil {
				sourceItems, _, _, _, err = parseFoundryRun(source.content, "source")
			}
			if err == nil {
				targetItems, _, _, _, err = parseFoundryRun(target.content, "target")
			}
			if err == nil {
				for _, id := range slices.Sorted(maps.Keys(sourceItems)) {
					s, t := sourceItems[id], targetItems[id]
					cases = append(cases, evaluationCase{
						caseID: id, question: s.dataset.query, sourceOutput: s.output, targetOutput: t.output,
						expectedOutput: datasetItems[id].expectedOutput,
						sourceLatency:  s.latency, targetLatency: t.latency,
						sourceInputTokens: s.inputTokens, sourceOutputTokens: s.outputTokens,
						targetInputTokens: t.inputTokens, targetOutputTokens: t.outputTokens,
						assessments: append(slices.Clone(s.assessments), t.assessments...),
					})
				}
			}
		}
	} else {
		file := files[plan.Source.File]
		rows := file.collections[plan.Source.Collection]
		if plan.Adapter == "canonical" {
			for _, row := range rows {
				var item evaluationCase
				item, err = caseFromCanonicalRecord(row)
				if err != nil {
					break
				}
				cases = append(cases, item)
			}
		} else {
			table := make([]map[string]string, 0, len(rows))
			for _, row := range rows {
				record := map[string]string{}
				for key, value := range row {
					record[key] = valueString(value)
				}
				table = append(table, record)
			}
			cases, err = casesFromRows(table)
		}
		if err == nil {
			analysis, err = buildEvaluationAnalysis(file.profile.Name, file.profile.Format, "", []string{}, cases)
		}
	}
	if err == nil {
		err = validateEvaluationModels(analysis, sourceModel, targetModel)
	}
	if err != nil {
		mappingIssue(&preview, "error", "NATIVE_VALIDATION", err.Error(), "")
		return preview, analysis
	}
	seen := map[string]bool{}
	for index := range cases {
		item := &cases[index]
		if seen[item.caseID] {
			mappingIssue(&preview, "error", "DUPLICATE_CASE_KEY", "duplicate native case ID", item.caseID)
		}
		seen[item.caseID] = true
		source := nativeSelectedAssessment(*item, plan.Source, plan.Adapter, "source", &preview)
		target := nativeSelectedAssessment(*item, plan.Target, plan.Adapter, "target", &preview)
		item.selectedQuality = new([3]string{source.status, target.status, target.rationale})
		item.selectedAssessments = new([2]evaluationAssessment{source, target})
		if plan.Adapter == "foundry" && target.status == "fail" {
			item.failureKind = inferFailureKind(target.rationale)
			item.failureDetail = target.rationale
			item.failureSource = "inferred"
		}
		if classifiedMappingStatus(source.status) && classifiedMappingStatus(target.status) {
			preview.ComparableCount++
		} else {
			preview.UnclassifiedCount++
			mappingIssue(&preview, "warning", "UNKNOWN_GRADE", "native case has no comparable quality pair", item.caseID)
		}
		appendMappingPreviewCase(&preview, *item, source.score, target.score)
	}
	for _, lane := range []struct {
		mapping  MappingLane
		expected string
		model    *string
		runID    *string
	}{
		{plan.Source, sourceModel, &analysis.SourceModel, &analysis.SourceRunID},
		{plan.Target, targetModel, &analysis.TargetModel, &analysis.TargetRunID},
	} {
		rows := files[lane.mapping.File].collections[lane.mapping.Collection]
		for _, row := range rows {
			if lane.mapping.Model != "" {
				if value, exists := pointerValue(row, lane.mapping.Model); exists && value != nil && value != "" {
					if model, ok := value.(string); !ok || !modelNamesCompatible(model, lane.expected) {
						mappingIssue(&preview, "error", "MODEL_IDENTITY_CONFLICT",
							"native reported model conflicts with binding", "")
					} else {
						preserveMappedIdentity(lane.model, model, "native model", "", &preview)
					}
				} else {
					mappingIssue(&preview, "warning", "UNVERIFIED_IDENTITY",
						"native model identity is missing on a row", "")
				}
			}
			if lane.mapping.RunID != "" {
				value, _ := pointerValue(row, lane.mapping.RunID)
				if value != nil && value != "" {
					if runID, ok := value.(string); ok {
						preserveMappedIdentity(lane.runID, runID, "native run", "", &preview)
					} else {
						mappingIssue(&preview, "error", "INVALID_IDENTITY", "native run ID must be a string", "")
					}
				}
			}
		}
	}
	reviewed, err := buildEvaluationAnalysis(
		analysis.FileName, analysis.Format, analysis.SheetName, analysis.DetectedFields, cases,
	)
	if err != nil {
		mappingIssue(&preview, "error", "NATIVE_VALIDATION", err.Error(), "")
		return preview, analysis
	}
	reviewed.SourceModel, reviewed.TargetModel = analysis.SourceModel, analysis.TargetModel
	reviewed.SourceRunID, reviewed.TargetRunID = analysis.SourceRunID, analysis.TargetRunID
	reviewed.EvaluationID = analysis.EvaluationID
	analysis = reviewed
	appendMappingProvenance(files, &preview, &analysis)
	preview.CaseCount = len(cases)
	if len(cases) > maxEvaluationRows || preview.ComparableCount == 0 {
		mappingIssue(&preview, "error", "CASE_LIMIT", "native evidence requires 1..10,000 cases and comparable grades", "")
	}

	preview.SourceModel, preview.TargetModel = analysis.SourceModel, analysis.TargetModel
	preview.SourceRunID, preview.TargetRunID = analysis.SourceRunID, analysis.TargetRunID
	if analysis.SourceModel == "" || analysis.TargetModel == "" {
		mappingIssue(&preview, "warning", "UNVERIFIED_MODEL", "native adapter does not report both model identities", "")
	}
	analysis.EvaluationSHA256 = preview.EvaluationSHA256
	analysis.ComparableCount, analysis.UnclassifiedCount = preview.ComparableCount, preview.UnclassifiedCount
	return preview, analysis
}

func nativeSelectedAssessment(
	item evaluationCase, lane MappingLane, adapter, role string, preview *MappingPreview,
) evaluationAssessment {
	name := "conclusion-accuracy"
	if adapter != "meera" {
		name = ""
		for _, filter := range lane.Evaluator.Filters {
			if filter.Path == "/evaluator" || filter.Path == "/name" {
				name = filter.Value
			}
		}
	}
	selected := evaluationAssessment{evaluator: name, subject: role, status: "unknown"}
	matches := 0
	for _, assessment := range item.assessments {
		if assessment.evaluator == name && assessment.subject == role {
			selected = assessment
			matches++
		}
	}
	if matches > 1 {
		mappingIssue(preview, "error", "AMBIGUOUS_EVALUATOR",
			"native quality selector matches multiple assessments", item.caseID)
	}
	if selected.status == "" {
		selected.status = "unknown"
	}
	return selected
}

func appendMappingProvenance(files []mappingFile, preview *MappingPreview, analysis *EvaluationAnalysis) {
	demo, synthetic, contextUnavailable, descriptionOnly := false, false, false, false
	for _, file := range files {
		for _, rows := range file.collections {
			for _, row := range rows {
				if _, exists := row["migration_demo_provenance"]; exists {
					demo = true
				}
				if value, exists := row["is_synthetic"]; exists {
					text, _ := mappingScalar(value)
					synthetic = synthetic || strings.EqualFold(text, "true")
				}
				if value, exists := row["actual_context_available"]; exists {
					text, _ := mappingScalar(value)
					contextUnavailable = contextUnavailable || strings.EqualFold(text, "false")
				}
				origin := strings.ToLower(valueString(row["question_origin"]))
				descriptionOnly = descriptionOnly || strings.Contains(origin, "requirement description only")
			}
		}
	}
	for _, warning := range []struct {
		present       bool
		code, message string
	}{
		{demo, "DEMO_RELABEL", "Demo-relabeled evidence is not a real evaluation of the relabeled model."},
		{synthetic, "SYNTHETIC_EVIDENCE",
			"Uploaded metadata marks outputs as synthetic; they do not validate model performance or prompt improvements."},
		{contextUnavailable, "CONTEXT_UNAVAILABLE", "Uploaded metadata reports actual runtime context is unavailable."},
		{descriptionOnly, "PARTIAL_INPUT",
			"Uploaded input provenance identifies Requirement Description only, not the full runtime Question."},
	} {
		if warning.present {
			mappingIssue(preview, "warning", warning.code, warning.message, "")
			analysis.Warnings = append(analysis.Warnings, warning.message)
		}
	}
}

func mappingPreviewError(preview MappingPreview) error {
	for _, issue := range preview.Issues {
		if issue.Severity == "error" {
			return fmt.Errorf("%s: %s", issue.Code, issue.Message)
		}
	}
	return errors.New("mapping is not valid")
}

func mappingProfiles(files []mappingFile) []MappingFileProfile {
	profiles := make([]MappingFileProfile, len(files))
	for index, file := range files {
		profiles[index] = file.profile
	}
	return profiles
}

func normalizeMapping(plan *EvaluationMapping) {
	for _, lane := range []*MappingLane{&plan.Source, &plan.Target} {
		if lane.Filters == nil {
			lane.Filters = []MappingFilter{}
		}
		if lane.Evaluator.Filters == nil {
			lane.Evaluator.Filters = []MappingFilter{}
		}
	}
	plan.Adapter = strings.TrimSpace(plan.Adapter)
}
