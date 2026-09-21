// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

// MappingField describes a local record field. Samples never enter an AI proposal.
type MappingField struct {
	Path    string   `json:"path"`
	Types   []string `json:"types"`
	Present int      `json:"present"`
	Sample  string   `json:"sample,omitempty"`
}

// MappingCollection identifies records by RFC 6901 pointer; XLSX uses /sheets/<escaped sheet name>.
type MappingCollection struct {
	Path     string         `json:"path"`
	RowCount int            `json:"rowCount"`
	Fields   []MappingField `json:"fields"`
}

// MappingFileProfile contains bounded, local-only structural and sample information.
type MappingFileProfile struct {
	Index       int                 `json:"index"`
	Name        string              `json:"name"`
	SHA256      string              `json:"sha256"`
	Format      string              `json:"format"`
	Collections []MappingCollection `json:"collections"`
}

// MappingFilter compares scalar lexical values exactly, without executing expressions.
type MappingFilter struct {
	Path  string `json:"path"`
	Value string `json:"value"`
}

// MappingEvaluator selects one assessment per record, or grade fields on the record itself.
type MappingEvaluator struct {
	Collection string          `json:"collection"`
	Filters    []MappingFilter `json:"filters"`
	Status     string          `json:"status"`
	Score      string          `json:"score"`
	Rationale  string          `json:"rationale"`
}

// MappingLane selects and interprets a single model run.
type MappingLane struct {
	File               int              `json:"file"`
	Collection         string           `json:"collection"`
	Filters            []MappingFilter  `json:"filters"`
	CaseID             string           `json:"caseId"`
	Input              string           `json:"input"`
	Context            string           `json:"context,omitempty"`
	ExpectedOutput     string           `json:"expectedOutput,omitempty"`
	ExpectedReferences string           `json:"expectedReferences,omitempty"`
	Output             string           `json:"output"`
	Model              string           `json:"model"`
	RunID              string           `json:"runId"`
	LatencyMs          string           `json:"latencyMs"`
	InputTokens        string           `json:"inputTokens"`
	OutputTokens       string           `json:"outputTokens"`
	Evaluator          MappingEvaluator `json:"evaluator"`
	PassRule           string           `json:"passRule"`
	Operator           string           `json:"operator"`
	Threshold          *float64         `json:"threshold,omitempty"`
}

// MappingDataset optionally supplies inputs and references joined by exact case ID.
type MappingDataset struct {
	File           int    `json:"file"`
	Collection     string `json:"collection"`
	CaseID         string `json:"caseId"`
	Input          string `json:"input"`
	Reference      string `json:"reference"`
	ExpectedOutput string `json:"expectedOutput,omitempty"`
}

// EvaluationMapping is a versioned data-only conversion plan.
type EvaluationMapping struct {
	Version int             `json:"version"`
	Adapter string          `json:"adapter"`
	Source  MappingLane     `json:"source"`
	Target  MappingLane     `json:"target"`
	Dataset *MappingDataset `json:"dataset,omitempty"`
}

// MappingIssue explains a blocking error or an acknowledged limitation.
type MappingIssue struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	CaseID   string `json:"caseId,omitempty"`
}

// MappingPreviewCase is a bounded display projection of paired evidence.
type MappingPreviewCase struct {
	CaseID       string   `json:"caseId"`
	Input        string   `json:"input"`
	SourceOutput string   `json:"sourceOutput"`
	TargetOutput string   `json:"targetOutput"`
	SourceStatus string   `json:"sourceStatus"`
	TargetStatus string   `json:"targetStatus"`
	SourceScore  *float64 `json:"sourceScore,omitempty"`
	TargetScore  *float64 `json:"targetScore,omitempty"`
}

// MappingPreview validates all rows while displaying at most twenty cases.
type MappingPreview struct {
	Valid             bool                 `json:"valid"`
	EvaluationSHA256  string               `json:"evaluationSha256"`
	CaseCount         int                  `json:"caseCount"`
	ComparableCount   int                  `json:"comparableCount"`
	UnclassifiedCount int                  `json:"unclassifiedCount"`
	SourceOnly        int                  `json:"sourceOnly"`
	TargetOnly        int                  `json:"targetOnly"`
	Issues            []MappingIssue       `json:"issues"`
	Cases             []MappingPreviewCase `json:"cases"`
	SourceModel       string               `json:"sourceModel,omitempty"`
	TargetModel       string               `json:"targetModel,omitempty"`
	SourceRunID       string               `json:"sourceRunId,omitempty"`
	TargetRunID       string               `json:"targetRunId,omitempty"`
	StartedAt         string               `json:"startedAt,omitempty"`
	CompletedAt       string               `json:"completedAt,omitempty"`
}

// MappingProfileResponse contains an unconfirmed mapping suggestion.
type MappingProfileResponse struct {
	Files    []MappingFileProfile `json:"files"`
	Mapping  EvaluationMapping    `json:"mapping"`
	Warnings []string             `json:"warnings"`
}

type mappingFile struct {
	profile     MappingFileProfile
	content     []byte
	document    any
	collections map[string][]map[string]any
}

func normalizeMappingProfileResponse(response *MappingProfileResponse) {
	if response.Files == nil {
		response.Files = []MappingFileProfile{}
	}
	if response.Warnings == nil {
		response.Warnings = []string{}
	}
	normalizeMapping(&response.Mapping)
	for index := range response.Files {
		file := &response.Files[index]
		if file.Collections == nil {
			file.Collections = []MappingCollection{}
		}
		for index := range file.Collections {
			collection := &file.Collections[index]
			if collection.Fields == nil {
				collection.Fields = []MappingField{}
			}
			for index := range collection.Fields {
				field := &collection.Fields[index]
				if field.Types == nil {
					field.Types = []string{}
				}
			}
		}
	}
}

func normalizeMappingPreview(preview *MappingPreview) {
	if preview.Issues == nil {
		preview.Issues = []MappingIssue{}
	}
	if preview.Cases == nil {
		preview.Cases = []MappingPreviewCase{}
	}
	preview.Valid = preview.Valid && preview.ComparableCount > 0 && preview.SourceOnly == 0 && preview.TargetOnly == 0
	for _, issue := range preview.Issues {
		if issue.Severity == "error" {
			preview.Valid = false
		}
	}
}
