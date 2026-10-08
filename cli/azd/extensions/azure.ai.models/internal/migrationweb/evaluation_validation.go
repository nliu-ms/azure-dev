// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
)

const candidatePromptProvenanceWarning = "The adapted Target file does not verify that it used the candidate prompt."

// EvaluationValidation compares a confirmed baseline Target run with one adapted Target rerun.
type EvaluationValidation struct {
	PromptSHA256             string         `json:"promptSha256"`
	BaselineEvaluationSHA256 string         `json:"baselineEvaluationSha256"`
	CandidatePromptSHA256    string         `json:"candidatePromptSha256"`
	AdaptedTargetSHA256      string         `json:"adaptedTargetSha256"`
	CaseCount                int            `json:"caseCount"`
	ResolvedCount            int            `json:"resolvedCount"`
	RemainingCount           int            `json:"remainingCount"`
	NewFailureCount          int            `json:"newFailureCount"`
	PreservedCount           int            `json:"preservedCount"`
	UnclassifiedCount        int            `json:"unclassifiedCount"`
	MissingCount             int            `json:"missingCount"`
	ExtraCount               int            `json:"extraCount"`
	ReadyForRolloutReview    bool           `json:"readyForRolloutReview"`
	ResolvedCaseIDs          []string       `json:"resolvedCaseIds"`
	RemainingCaseIDs         []string       `json:"remainingCaseIds"`
	NewFailureCaseIDs        []string       `json:"newFailureCaseIds"`
	UnclassifiedCaseIDs      []string       `json:"unclassifiedCaseIds"`
	Issues                   []MappingIssue `json:"issues"`
	Warnings                 []string       `json:"warnings"`
}

func readSingleMappingFile(r *http.Request, field string) (mappingFile, error) {
	if r.MultipartForm == nil || len(r.MultipartForm.File[field]) != 1 {
		return mappingFile{}, fmt.Errorf("provide exactly one %s file", field)
	}
	header := r.MultipartForm.File[field][0]
	file, err := header.Open()
	if err != nil {
		return mappingFile{}, fmt.Errorf("open %s file: %w", field, err)
	}
	content, readErr := io.ReadAll(io.LimitReader(file, maxEvaluationUploadBytes+1))
	closeErr := file.Close()
	if readErr != nil {
		return mappingFile{}, fmt.Errorf("read %s file: %w", field, readErr)
	}
	if closeErr != nil {
		return mappingFile{}, fmt.Errorf("close %s file: %w", field, closeErr)
	}
	if len(content) == 0 || len(content) > maxEvaluationUploadBytes {
		return mappingFile{}, fmt.Errorf("%s file is empty or exceeds the 24 MB limit", field)
	}
	result, err := profileMappingFile(0, filepath.Base(header.Filename), content)
	if err != nil {
		return mappingFile{}, fmt.Errorf("profile %s file: %w", field, err)
	}
	return result, nil
}

func hasMatchingPromptHash(file mappingFile, promptHash string) bool {
	matched := false
	check := func(value any) {
		declared, present := pointerValue(value, "/suite/prompt_sha256")
		hash, ok := declared.(string)
		if present && ok && len(hash) == 64 {
			_, valid := hex.DecodeString(hash)
			matched = matched || (valid == nil && strings.EqualFold(hash, promptHash))
		}
	}
	check(file.document)
	for _, rows := range file.collections {
		for _, row := range rows {
			check(row)
		}
	}
	return matched
}

func addValidationIssue(result *EvaluationValidation, severity, code, message, caseID string) {
	result.Issues = append(result.Issues, MappingIssue{
		Severity: severity,
		Code:     code,
		Message:  message,
		CaseID:   caseID,
	})
}

func validateAdaptedTargetMetadata(
	file mappingFile, expectedModel, observedModel, candidateHash string,
	preview *MappingPreview, result *EvaluationValidation,
) {
	validateMappingPromptMetadataFor([]mappingFile{file}, candidateHash, true, "candidate prompt", preview)
	if !hasMatchingPromptHash(file, candidateHash) {
		result.Warnings = append(result.Warnings, candidatePromptProvenanceWarning)
	}
	value, present := pointerValue(file.document, "/runs/target/model")
	if !present {
		return
	}
	model, ok := value.(string)
	if !ok || strings.TrimSpace(model) == "" {
		mappingIssue(preview, "error", "INVALID_IDENTITY",
			"target envelope model must be a nonempty string", "")
	} else if !modelNamesCompatible(model, expectedModel) || !modelNamesCompatible(model, observedModel) {
		mappingIssue(preview, "error", "MODEL_IDENTITY_CONFLICT",
			"target envelope model conflicts with the selected binding or record metadata", "")
	}
}

func compareValidationCases(
	baseline, adapted map[string]mappedObservation, preview *MappingPreview, result *EvaluationValidation,
) {
	result.CaseCount = len(baseline)
	keys := map[string]bool{}
	for key := range baseline {
		keys[key] = true
	}
	for key := range adapted {
		keys[key] = true
	}
	for _, key := range slices.Sorted(maps.Keys(keys)) {
		before, hasBaseline := baseline[key]
		after, hasAdapted := adapted[key]
		caseID := before.caseID
		if !hasBaseline {
			caseID = after.caseID
			result.ExtraCount++
			addValidationIssue(result, "error", "EXTRA_CASE", "case is not present in the baseline Target run", caseID)
			continue
		}
		if !hasAdapted {
			result.MissingCount++
			addValidationIssue(result, "error", "MISSING_CASE", "case is missing from the adapted Target run", caseID)
			continue
		}
		if valueStringJSON(before.inputValue) != valueStringJSON(after.inputValue) {
			addValidationIssue(result, "error", "INPUT_CONFLICT",
				"adapted Target input differs from the baseline Target input", caseID)
		}
		if !classifiedMappingStatus(before.status) || !classifiedMappingStatus(after.status) {
			result.UnclassifiedCount++
			result.UnclassifiedCaseIDs = append(result.UnclassifiedCaseIDs, caseID)
			continue
		}
		switch {
		case before.status == "fail" && after.status == "pass":
			result.ResolvedCount++
			result.ResolvedCaseIDs = append(result.ResolvedCaseIDs, caseID)
		case before.status == "fail":
			result.RemainingCount++
			result.RemainingCaseIDs = append(result.RemainingCaseIDs, caseID)
		case after.status == "fail":
			result.NewFailureCount++
			result.NewFailureCaseIDs = append(result.NewFailureCaseIDs, caseID)
		default:
			result.PreservedCount++
		}
	}
	result.Issues = append(result.Issues, preview.Issues...)
	result.ReadyForRolloutReview = result.RemainingCount == 0 &&
		result.NewFailureCount == 0 &&
		result.UnclassifiedCount == 0 &&
		result.MissingCount == 0 &&
		result.ExtraCount == 0 &&
		!slices.ContainsFunc(result.Issues, func(issue MappingIssue) bool {
			return issue.Severity == "error"
		})
}

func (s *Server) handleEvaluationValidation(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxEvaluationUploadBytes)
	if err := r.ParseMultipartForm(4 * 1024 * 1024); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Validation evidence must be multipart form data under the 24 MB total limit.",
		})
		return
	}
	defer r.MultipartForm.RemoveAll()

	_, prompt, err := readUploadedFile(r, "prompt")
	if err != nil || len(strings.TrimSpace(string(prompt))) == 0 {
		if err == nil {
			err = errors.New("the Source prompt file is empty")
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	candidatePrompt := r.FormValue("candidatePrompt")
	if len(strings.TrimSpace(candidatePrompt)) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "candidatePrompt is required and must be nonempty"})
		return
	}
	if len(candidatePrompt) > maxPromptBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("candidatePrompt exceeds the %d KB limit", maxPromptBytes/1024),
		})
		return
	}
	if len(r.MultipartForm.File["files"]) == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error": "Validate currently requires confirmed generic mapped baseline input using the repeated files field.",
		})
		return
	}

	_, baselineDigest, err := analyzeMappedUpload(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	files, err := readMappingFiles(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	plan, err := decodeEvaluationMapping(r.FormValue("mapping"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if plan.Adapter != "generic" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error": "Validate currently requires generic mapped baseline input.",
		})
		return
	}
	adaptedFile, err := readSingleMappingFile(r, "adaptedTarget")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	targetModel := r.FormValue("targetModelName")
	baselinePreview := MappingPreview{Valid: true, Issues: []MappingIssue{}, Cases: []MappingPreviewCase{}}
	baselineTarget := mappedLane(files, plan.Target, "Target", plan.Dataset != nil, targetModel, &baselinePreview)
	adaptedModel := strings.TrimSpace(r.FormValue("adaptedTargetModelName"))
	if adaptedModel == "" {
		adaptedModel = targetModel
	}
	adaptedPlan := plan.Target
	adaptedPlan.File = 0
	adaptedPreview := MappingPreview{Valid: true, Issues: []MappingIssue{}, Cases: []MappingPreviewCase{}}
	adaptedTarget := mappedLane(
		[]mappingFile{adaptedFile}, adaptedPlan, "Target", false, adaptedModel, &adaptedPreview)
	var observedModel string
	for _, item := range adaptedTarget {
		preserveMappedIdentity(&observedModel, item.model, "Target model", item.caseID, &adaptedPreview)
	}

	promptDigest := sha256.Sum256(prompt)
	candidateDigest := sha256.Sum256([]byte(candidatePrompt))
	result := EvaluationValidation{
		PromptSHA256:             fmt.Sprintf("%x", promptDigest),
		BaselineEvaluationSHA256: baselineDigest,
		CandidatePromptSHA256:    fmt.Sprintf("%x", candidateDigest),
		AdaptedTargetSHA256:      adaptedFile.profile.SHA256,
		ResolvedCaseIDs:          []string{},
		RemainingCaseIDs:         []string{},
		NewFailureCaseIDs:        []string{},
		UnclassifiedCaseIDs:      []string{},
		Issues:                   []MappingIssue{},
		Warnings:                 []string{},
	}
	validateAdaptedTargetMetadata(
		adaptedFile, adaptedModel, observedModel, result.CandidatePromptSHA256, &adaptedPreview, &result)
	compareValidationCases(baselineTarget, adaptedTarget, &adaptedPreview, &result)
	writeJSON(w, http.StatusOK, result)
}
