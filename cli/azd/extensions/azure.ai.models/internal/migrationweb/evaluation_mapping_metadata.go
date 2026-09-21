// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"encoding/hex"
	"maps"
	"slices"
	"strings"
	"time"
)

func applyMappingRunMetadata(
	files []mappingFile, plan EvaluationMapping, sourceModel, targetModel string,
	preview *MappingPreview, analysis *EvaluationAnalysis,
) {
	var windowStart, windowEnd time.Time
	completeWindows := 0
	for _, role := range []struct {
		name     string
		file     int
		expected string
		model    *string
		runID    *string
	}{
		{"source", plan.Source.File, sourceModel, &preview.SourceModel, &preview.SourceRunID},
		{"target", plan.Target.File, targetModel, &preview.TargetModel, &preview.TargetRunID},
	} {
		if role.file < 0 || role.file >= len(files) {
			continue
		}
		document := files[role.file].document
		base := "/runs/" + role.name
		startValue, hasStart := pointerValue(document, base+"/started_at")
		endValue, hasEnd := pointerValue(document, base+"/completed_at")
		if hasStart || hasEnd {
			start, startErr := time.Parse(time.RFC3339Nano, valueString(startValue))
			end, endErr := time.Parse(time.RFC3339Nano, valueString(endValue))
			if startErr != nil || endErr != nil || !start.Before(end) {
				mappingIssue(preview, "warning", "INVALID_RUN_TIME",
					role.name+" run window is incomplete or invalid; select the Monitor window manually", "")
			} else {
				completeWindows++
				if windowStart.IsZero() || start.Before(windowStart) {
					windowStart = start
				}
				if windowEnd.IsZero() || end.After(windowEnd) {
					windowEnd = end
				}
			}
		}
		if value, present := pointerValue(document, base+"/model"); present {
			model, ok := value.(string)
			if !ok || model == "" {
				mappingIssue(preview, "error", "INVALID_IDENTITY", role.name+" envelope model must be a nonempty string", "")
			} else if !modelNamesCompatible(model, role.expected) || !modelNamesCompatible(model, *role.model) {
				mappingIssue(preview, "error", "MODEL_IDENTITY_CONFLICT",
					role.name+" envelope model conflicts with the selected binding or record metadata", "")
			} else if *role.model == "" {
				*role.model = model
			}
		}
		if value, present := pointerValue(document, base+"/run_id"); present {
			if runID, ok := value.(string); ok && runID != "" {
				preserveMappedIdentity(role.runID, runID, role.name+" envelope run ID", "", preview)
			} else {
				mappingIssue(preview, "error", "INVALID_IDENTITY",
					role.name+" envelope run ID must be a nonempty string", "")
			}
		}
	}
	if completeWindows == 2 {
		preview.StartedAt = windowStart.UTC().Format(time.RFC3339Nano)
		preview.CompletedAt = windowEnd.UTC().Format(time.RFC3339Nano)
	}
	analysis.SourceModel, analysis.TargetModel = preview.SourceModel, preview.TargetModel
	analysis.SourceRunID, analysis.TargetRunID = preview.SourceRunID, preview.TargetRunID
	if preview.SourceModel != "" && preview.TargetModel != "" {
		preview.Issues = slices.DeleteFunc(preview.Issues, func(issue MappingIssue) bool {
			return issue.Code == "UNVERIFIED_MODEL"
		})
		analysis.Warnings = slices.DeleteFunc(analysis.Warnings, func(warning string) bool {
			return warning == "model identity is unreported; confirmation attests the selected model bindings"
		})
	}
}

func validateMappingPromptMetadata(files []mappingFile, promptHash string, promptPresent bool, preview *MappingPreview) {
	for _, file := range files {
		validate := func(value any, caseID string) {
			declared, present := pointerValue(value, "/suite/prompt_sha256")
			if !present {
				return
			}
			hash, ok := declared.(string)
			if !ok || len(hash) != 64 {
				mappingIssue(preview, "error", "INVALID_PROMPT_HASH",
					"suite.prompt_sha256 must be a 64-character SHA-256 hex digest", caseID)
				return
			}
			if _, err := hex.DecodeString(hash); err != nil {
				mappingIssue(preview, "error", "INVALID_PROMPT_HASH",
					"suite.prompt_sha256 must be a 64-character SHA-256 hex digest", caseID)
				return
			}
			if !promptPresent {
				mappingIssue(preview, "warning", "UNVERIFIED_PROMPT",
					"upload the Source prompt to verify suite.prompt_sha256", caseID)
			} else if !strings.EqualFold(hash, promptHash) {
				mappingIssue(preview, "error", "PROMPT_HASH_MISMATCH",
					"suite.prompt_sha256 does not match the uploaded Source prompt", caseID)
			}
		}
		validate(file.document, "")
		for _, collection := range slices.Sorted(maps.Keys(file.collections)) {
			for _, row := range file.collections[collection] {
				validate(row, stringAt(row, "case_id"))
			}
		}
	}
}
