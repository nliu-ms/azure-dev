// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

const mappingProposalTimeout = 45 * time.Second

func readMappingFiles(r *http.Request) ([]mappingFile, error) {
	if r.MultipartForm == nil {
		return nil, errors.New("multipart files are required")
	}
	headers := r.MultipartForm.File["files"]
	if len(headers) < 1 || len(headers) > 3 {
		return nil, errors.New("provide between 1 and 3 evaluation files using the repeated files field")
	}
	total := int64(0)
	for _, fieldHeaders := range r.MultipartForm.File {
		for _, header := range fieldHeaders {
			total += header.Size
		}
	}
	if total > maxEvaluationUploadBytes {
		return nil, errors.New("uploaded files exceed the 24 MB total limit")
	}
	files := make([]mappingFile, 0, len(headers))
	for index, header := range headers {
		file, err := header.Open()
		if err != nil {
			return nil, fmt.Errorf("open evaluation file: %w", err)
		}
		content, readErr := io.ReadAll(io.LimitReader(file, maxEvaluationUploadBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read evaluation file: %w", readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close evaluation file: %w", closeErr)
		}
		if len(content) == 0 || len(content) > maxEvaluationUploadBytes {
			return nil, errors.New("evaluation file is empty or exceeds the 24 MB limit")
		}
		profile, err := profileMappingFile(index, filepath.Base(header.Filename), content)
		if err != nil {
			return nil, fmt.Errorf("profile file %d: %w", index+1, err)
		}
		files = append(files, profile)
	}
	return files, nil
}

func decodeEvaluationMapping(content string) (EvaluationMapping, error) {
	var plan EvaluationMapping
	if len(content) == 0 || len(content) > 64*1024 {
		return plan, errors.New("mapping must be JSON under the 64 KB limit")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return plan, fmt.Errorf("invalid mapping JSON: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return plan, errors.New("mapping must contain exactly one JSON object")
	}
	normalizeMapping(&plan)
	return plan, nil
}

func bindMappingPrompt(
	r *http.Request, files []mappingFile, preview *MappingPreview, analysis *EvaluationAnalysis,
) error {
	var prompt []byte
	promptPresent := r.MultipartForm != nil && len(r.MultipartForm.File["prompt"]) > 0
	if promptPresent {
		var err error
		_, prompt, err = readUploadedFile(r, "prompt")
		if err != nil {
			return err
		}
		if len(prompt) > maxPromptBytes {
			return errors.New("Source prompt exceeds the 256 KB limit")
		}
	}
	promptHash := sha256.Sum256(prompt)
	validateMappingPromptMetadata(files, fmt.Sprintf("%x", promptHash), promptPresent, preview)
	bound := sha256.Sum256([]byte(fmt.Sprintf("%s:%x", preview.EvaluationSHA256, promptHash)))
	preview.EvaluationSHA256 = fmt.Sprintf("%x", bound)
	analysis.EvaluationSHA256 = preview.EvaluationSHA256
	analysis.PromptSHA256 = fmt.Sprintf("%x", promptHash)
	return nil
}

func analyzeMappedUpload(r *http.Request) (EvaluationAnalysis, string, error) {
	files, err := readMappingFiles(r)
	if err != nil {
		return EvaluationAnalysis{}, "", err
	}
	plan, err := decodeEvaluationMapping(r.FormValue("mapping"))
	if err != nil {
		return EvaluationAnalysis{}, "", err
	}
	if r.FormValue("mappingConfirmed") != "true" {
		return EvaluationAnalysis{}, "", errors.New("review the full-data preview and confirm the mapping before analysis")
	}
	preview, analysis := previewMapping(files, plan, r.FormValue("sourceModelName"), r.FormValue("targetModelName"))
	if err := bindMappingPrompt(r, files, &preview, &analysis); err != nil {
		return EvaluationAnalysis{}, "", err
	}
	if !preview.Valid {
		return EvaluationAnalysis{}, "", mappingPreviewError(preview)
	}
	if r.FormValue("evaluationSha256") != preview.EvaluationSHA256 {
		return EvaluationAnalysis{}, "", errors.New(
			"STALE_MAPPING: files, mapping, prompt or model bindings changed; preview again")
	}
	return analysis, preview.EvaluationSHA256, nil
}

func (s *Server) handleMappingRequest(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxEvaluationUploadBytes)
	if err := r.ParseMultipartForm(4 * 1024 * 1024); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "provide multipart files under the 24 MB total limit",
		})
		return
	}
	defer r.MultipartForm.RemoveAll()
	files, err := readMappingFiles(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	source, target := r.FormValue("sourceModelName"), r.FormValue("targetModelName")
	plan, warnings := suggestMapping(files, source, target)
	switch r.URL.Path {
	case "/api/evaluation-mapping/preview":
		plan, err = decodeEvaluationMapping(r.FormValue("mapping"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		preview, analysis := previewMapping(files, plan, source, target)
		if err := bindMappingPrompt(r, files, &preview, &analysis); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, preview)
		return
	case "/api/evaluation-mapping/propose":
		if r.FormValue("allowAI") != "true" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "AI mapping requires explicit allowAI=true consent",
			})
			return
		}
		if s.mappingProposer == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "AI mapping is unavailable; use the manual editor",
			})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), mappingProposalTimeout)
		defer cancel()
		input := MappingProposalInput{
			Files:          mappingStructuralProfile(files),
			AccountName:    strings.TrimSpace(r.FormValue("optimizerAccountName")),
			ModelName:      strings.TrimSpace(r.FormValue("optimizerModelName")),
			DeploymentName: strings.TrimSpace(r.FormValue("optimizerDeploymentName")),
		}
		if !azureOpenAIAccountName.MatchString(input.AccountName) || input.ModelName == "" || input.DeploymentName == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "valid optimizer account, model and deployment are required",
			})
			return
		}
		plan, err = s.mappingProposer.Propose(ctx, input)
		if err == nil {
			normalizeMapping(&plan)
			err = validateMappingProposal(plan, input.Files)
		}
		if err == nil {
			err = resolveMappingProposalCollections(&plan, files)
		}
		if err != nil {
			status := http.StatusBadGateway
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				status = http.StatusGatewayTimeout
			}
			writeJSON(w, status, map[string]string{
				"error": "AI mapping proposal failed; manual mapping remains available: " + err.Error(),
			})
			return
		}
		warnings = append(warnings,
			"AI suggestion is unconfirmed; schema-only proposals cannot infer filter values or thresholds.")
	}
	response := MappingProfileResponse{Files: mappingProfiles(files), Mapping: plan, Warnings: warnings}
	normalizeMappingProfileResponse(&response)
	writeJSON(w, http.StatusOK, response)
}
