// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

type ServerOptions struct {
	Port            int
	SubscriptionID  string
	Provider        ModelProvider
	PromptOptimizer PromptOptimizer
}

type Server struct {
	listener        net.Listener
	httpServer      *http.Server
	subscriptionID  string
	provider        ModelProvider
	promptOptimizer PromptOptimizer
	token           string
}

const inventoryRequestTimeout = 60 * time.Second
const resourceRequestTimeout = 20 * time.Second
const metricsRequestTimeout = 30 * time.Second
const promptOptimizationTimeout = 120 * time.Second
const maxEvaluationUploadBytes = 24 * 1024 * 1024

func NewServer(options ServerOptions) (*Server, error) {
	if options.Provider == nil {
		return nil, errors.New("model provider is required")
	}
	token, err := newSessionToken()
	if err != nil {
		return nil, fmt.Errorf("create local session token: %w", err)
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", options.Port))
	if err != nil {
		return nil, fmt.Errorf("start local migration server: %w", err)
	}

	server := &Server{
		listener:        listener,
		subscriptionID:  options.SubscriptionID,
		provider:        options.Provider,
		promptOptimizer: options.PromptOptimizer,
		token:           token,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/resources", server.authorize(server.handleResources))
	mux.HandleFunc("POST /api/resource-models", server.authorize(server.handleResourceModels))
	mux.HandleFunc("GET /api/models", server.authorize(server.handleModels))
	mux.HandleFunc("GET /api/health", server.authorize(server.handleHealth))
	mux.HandleFunc("POST /api/recommendations", server.authorize(server.handleRecommendation))
	mux.HandleFunc("POST /api/assessments", server.authorize(server.handleAssessment))
	mux.HandleFunc("POST /api/deployment-options", server.authorize(server.handleDeploymentOptions))
	mux.HandleFunc("POST /api/deployment-metrics", server.authorize(server.handleDeploymentMetrics))
	mux.HandleFunc("POST /api/evaluation-analysis", server.authorize(server.handleEvaluationAnalysis))
	mux.HandleFunc("POST /api/prompt-optimization", server.authorize(server.handlePromptOptimization))
	mux.Handle("/", assetHandler(assets()))
	server.httpServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return server, nil
}

func (s *Server) URL() string {
	return "http://" + s.listener.Addr().String()
}

func (s *Server) BrowserURL() string {
	return s.URL() + "/?token=" + s.token
}

func (s *Server) Serve(ctx context.Context) error {
	serverError := make(chan error, 1)
	go func() {
		err := s.httpServer.Serve(s.listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serverError <- err
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("stop local migration server: %w", err)
		}
		return nil
	case err := <-serverError:
		if err != nil {
			return fmt.Errorf("serve local migration UI: %w", err)
		}
		return nil
	}
}

func (s *Server) authorize(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "The local migration session token is missing or invalid.",
			})
			return
		}
		next(w, r)
	}
}

func (s *Server) handleResources(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), inventoryRequestTimeout)
	defer cancel()
	result, err := s.provider.ListResources(ctx)
	if err != nil {
		status := http.StatusBadGateway
		message := fmt.Sprintf("Could not load Azure AI resources: %v", err)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
			message = "Azure AI resource discovery timed out. Retry the scan or choose another subscription."
		}
		writeJSON(w, status, map[string]string{"error": message})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleResourceModels(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var resource ModelAccount
	if err := json.NewDecoder(r.Body).Decode(&resource); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "The Azure AI resource request must contain valid JSON.",
		})
		return
	}
	resource.Name = strings.TrimSpace(resource.Name)
	resource.Kind = strings.TrimSpace(resource.Kind)
	resource.ResourceGroup = strings.TrimSpace(resource.ResourceGroup)
	resource.Location = strings.TrimSpace(resource.Location)
	resource.ResourceID = strings.TrimSpace(resource.ResourceID)
	if resource.Name == "" ||
		resource.Kind == "" ||
		resource.ResourceGroup == "" ||
		resource.Location == "" ||
		resource.ResourceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "name, kind, resourceGroup, location, and resourceId are required.",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), resourceRequestTimeout)
	defer cancel()
	result, err := s.provider.ListResourceDeployments(ctx, resource)
	if err != nil {
		status := http.StatusBadGateway
		message := fmt.Sprintf("Could not load deployments for %q: %v", resource.Name, err)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
			message = fmt.Sprintf("Timed out scanning Azure AI resource %q.", resource.Name)
		}
		writeJSON(w, status, map[string]string{"error": message})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), inventoryRequestTimeout)
	defer cancel()
	result, err := s.provider.ListDeployments(ctx)
	if err != nil {
		status := http.StatusBadGateway
		message := fmt.Sprintf("Could not load Azure model deployments: %v", err)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
			message = "Azure model discovery timed out. Retry the scan or use a subscription with fewer AI resources."
		}
		writeJSON(w, status, map[string]string{"error": message})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":         "ready",
		"subscriptionId": s.subscriptionID,
	})
}

func (s *Server) handleRecommendation(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var request ReplacementRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "The replacement request must contain valid JSON.",
		})
		return
	}
	request.ModelName = strings.TrimSpace(request.ModelName)
	if request.ModelName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "modelName is required.",
		})
		return
	}

	writeJSON(w, http.StatusOK, ReplacementRecommendation{
		SourceModel:     request.ModelName,
		SuggestedModel:  "gpt-5.4",
		SuggestedFormat: "OpenAI",
		Source:          "default",
	})
}

func (s *Server) handleAssessment(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var request AssessmentRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "The assessment request must contain valid JSON.",
		})
		return
	}
	request.ResourceGroup = strings.TrimSpace(request.ResourceGroup)
	request.AccountName = strings.TrimSpace(request.AccountName)
	request.TargetModel = strings.TrimSpace(request.TargetModel)
	request.TargetFormat = strings.TrimSpace(request.TargetFormat)
	if request.ResourceGroup == "" ||
		request.AccountName == "" ||
		request.TargetModel == "" ||
		request.TargetFormat == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "resourceGroup, accountName, targetModel, and targetFormat are required.",
		})
		return
	}

	result, err := s.provider.AssessTarget(r.Context(), request)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": fmt.Sprintf("Could not assess the target model: %v", err),
		})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleDeploymentOptions(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var request DeploymentOptionsRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "The deployment options request must contain valid JSON.",
		})
		return
	}
	request.ResourceGroup = strings.TrimSpace(request.ResourceGroup)
	request.AccountName = strings.TrimSpace(request.AccountName)
	request.Region = strings.TrimSpace(request.Region)
	request.TargetModel = strings.TrimSpace(request.TargetModel)
	request.TargetFormat = strings.TrimSpace(request.TargetFormat)
	if request.ResourceGroup == "" ||
		request.AccountName == "" ||
		request.Region == "" ||
		request.TargetModel == "" ||
		request.TargetFormat == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "resourceGroup, accountName, region, targetModel, and targetFormat are required.",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), resourceRequestTimeout)
	defer cancel()
	result, err := s.provider.AssessDeploymentOptions(ctx, request)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": fmt.Sprintf("Could not load deployment options: %v", err),
		})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleDeploymentMetrics(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var request DeploymentMetricsRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "The deployment metrics request must contain valid JSON.",
		})
		return
	}
	request.Source = normalizeMetricsDeployment(request.Source)
	request.Target = normalizeMetricsDeployment(request.Target)
	if !validMetricsDeployment(request.Source) || !validMetricsDeployment(request.Target) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Source and Target resourceId, location, and deploymentName are required.",
		})
		return
	}
	if request.StartTime.IsZero() ||
		request.EndTime.IsZero() ||
		!request.StartTime.Before(request.EndTime) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "startTime and endTime must define a valid UTC time window.",
		})
		return
	}
	if request.EndTime.Sub(request.StartTime) > 31*24*time.Hour {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "The telemetry window cannot exceed 31 days.",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), metricsRequestTimeout)
	defer cancel()
	result, err := s.provider.QueryDeploymentMetrics(ctx, request)
	if err != nil {
		status := http.StatusBadGateway
		message := fmt.Sprintf("Could not query Azure Monitor metrics: %v", err)
		if responseError, ok := errors.AsType[*azcore.ResponseError](err); ok &&
			responseError.StatusCode == http.StatusForbidden {
			status = http.StatusForbidden
			message = "Azure Monitor access was denied. Grant Monitoring Reader on the Source and Target accounts."
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
			message = "Azure Monitor metrics query timed out."
		}
		writeJSON(w, status, map[string]string{"error": message})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleEvaluationAnalysis(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxEvaluationUploadBytes)
	if err := r.ParseMultipartForm(4 * 1024 * 1024); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Prompt and evaluation evidence must be provided as multipart form data under the 24 MB total limit.",
		})
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	_, prompt, err := readUploadedFile(r, "prompt")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(strings.TrimSpace(string(prompt))) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "The Source prompt file is empty."})
		return
	}
	result, evaluationSHA256, err := analyzeUploadedEvaluation(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := validateEvaluationModels(
		result,
		r.FormValue("sourceModelName"),
		r.FormValue("targetModelName"),
	); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	promptDigest := sha256.Sum256(prompt)
	result.PromptSHA256 = fmt.Sprintf("%x", promptDigest)
	result.EvaluationSHA256 = evaluationSHA256
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handlePromptOptimization(w http.ResponseWriter, r *http.Request) {
	if s.promptOptimizer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "PromptV2 is not configured for this migration session.",
		})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxEvaluationUploadBytes)
	if err := r.ParseMultipartForm(4 * 1024 * 1024); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Prompt and evaluation evidence must be provided as multipart form data under the 24 MB total limit.",
		})
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	_, prompt, err := readUploadedFile(r, "prompt")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(prompt) > maxPromptBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("The Source prompt exceeds the %d KB limit.", maxPromptBytes/1024),
		})
		return
	}
	analysis, evaluationSHA256, err := analyzeUploadedEvaluation(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := validateEvaluationModels(
		analysis,
		r.FormValue("sourceModelName"),
		r.FormValue("targetModelName"),
	); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	target := ModelDeployment{
		ModelName: strings.TrimSpace(r.FormValue("targetModelName")),
	}
	optimizer := ModelDeployment{
		AccountName:    strings.TrimSpace(r.FormValue("optimizerAccountName")),
		ModelName:      strings.TrimSpace(r.FormValue("optimizerModelName")),
		DeploymentName: strings.TrimSpace(r.FormValue("optimizerDeploymentName")),
	}
	input, err := buildPromptOptimizationInput(
		string(prompt),
		strings.TrimSpace(r.FormValue("sourceModelName")),
		target,
		optimizer,
		analysis,
	)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), promptOptimizationTimeout)
	defer cancel()
	result, err := s.promptOptimizer.Optimize(ctx, input)
	if err != nil {
		status := http.StatusBadGateway
		message := fmt.Sprintf("Could not optimize the prompt: %v", err)
		if promptError, ok := errors.AsType[*PromptV2Error](err); ok {
			switch promptError.StatusCode {
			case http.StatusUnauthorized, http.StatusForbidden:
				status = http.StatusForbidden
				message = "PromptV2 access was denied. Sign in with an identity that can access the optimizer Foundry resource."
			case http.StatusUnprocessableEntity:
				status = http.StatusUnprocessableEntity
				message = "PromptV2 does not support the selected optimizer deployment or optimization request."
			}
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
			message = "PromptV2 optimization timed out."
		}
		writeJSON(w, status, map[string]string{"error": message})
		return
	}
	promptDigest := sha256.Sum256(prompt)
	result.PromptSHA256 = fmt.Sprintf("%x", promptDigest)
	result.EvaluationSHA256 = evaluationSHA256
	writeJSON(w, http.StatusOK, result)
}

func readUploadedFile(r *http.Request, field string) (string, []byte, error) {
	file, header, err := r.FormFile(field)
	if err != nil {
		return "", nil, fmt.Errorf("%s file is required", field)
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxEvaluationUploadBytes+1))
	if err != nil {
		return "", nil, fmt.Errorf("read %s file: %w", field, err)
	}
	if len(content) > maxEvaluationUploadBytes {
		return "", nil, fmt.Errorf("%s file exceeds the 24 MB limit", field)
	}
	return filepath.Base(header.Filename), content, nil
}

func analyzeUploadedEvaluation(r *http.Request) (EvaluationAnalysis, string, error) {
	if r.MultipartForm != nil && len(r.MultipartForm.File["evaluation"]) > 0 {
		evaluationName, evaluation, err := readUploadedFile(r, "evaluation")
		if err != nil {
			return EvaluationAnalysis{}, "", err
		}
		result, err := analyzeEvaluation(evaluationName, evaluation)
		if err != nil {
			return EvaluationAnalysis{}, "", fmt.Errorf(
				"Could not analyze %q: %w",
				evaluationName,
				err,
			)
		}
		evaluationDigest := sha256.Sum256(evaluation)
		return result, fmt.Sprintf("%x", evaluationDigest), nil
	}

	datasetName, dataset, err := readUploadedFile(r, "dataset")
	if err != nil {
		return EvaluationAnalysis{}, "", errors.New(
			"Provide either one evaluation file or the complete dataset, sourceEvaluation, and targetEvaluation bundle.",
		)
	}
	sourceName, source, err := readUploadedFile(r, "sourceEvaluation")
	if err != nil {
		return EvaluationAnalysis{}, "", err
	}
	targetName, target, err := readUploadedFile(r, "targetEvaluation")
	if err != nil {
		return EvaluationAnalysis{}, "", err
	}
	result, err := analyzeFoundryEvaluationBundle(
		datasetName,
		dataset,
		sourceName,
		source,
		targetName,
		target,
	)
	if err != nil {
		return EvaluationAnalysis{}, "", fmt.Errorf("Could not analyze the Foundry bundle: %w", err)
	}
	datasetDigest := sha256.Sum256(dataset)
	sourceDigest := sha256.Sum256(source)
	targetDigest := sha256.Sum256(target)
	joinedDigests := fmt.Sprintf(
		"%x:%x:%x",
		datasetDigest,
		sourceDigest,
		targetDigest,
	)
	bundleDigest := sha256.Sum256([]byte(joinedDigests))
	return result, fmt.Sprintf("%x", bundleDigest), nil
}

func validateEvaluationModels(
	analysis EvaluationAnalysis,
	expectedSource string,
	expectedTarget string,
) error {
	if !modelNamesCompatible(analysis.SourceModel, expectedSource) {
		return fmt.Errorf(
			"Source results model %q does not match the selected Source model %q",
			analysis.SourceModel,
			strings.TrimSpace(expectedSource),
		)
	}
	if !modelNamesCompatible(analysis.TargetModel, expectedTarget) {
		return fmt.Errorf(
			"Target results model %q does not match the selected Target model %q",
			analysis.TargetModel,
			strings.TrimSpace(expectedTarget),
		)
	}
	return nil
}

func modelNamesCompatible(actual string, expected string) bool {
	actual = strings.ToLower(strings.TrimSpace(actual))
	expected = strings.ToLower(strings.TrimSpace(expected))
	if actual == "" || expected == "" {
		return true
	}
	return actual == expected ||
		strings.HasPrefix(actual, expected+"-") ||
		strings.HasPrefix(expected, actual+"-")
}

func normalizeMetricsDeployment(deployment MetricsDeployment) MetricsDeployment {
	deployment.ResourceID = strings.TrimSpace(deployment.ResourceID)
	deployment.Location = strings.TrimSpace(deployment.Location)
	deployment.DeploymentName = strings.TrimSpace(deployment.DeploymentName)
	return deployment
}

func validMetricsDeployment(deployment MetricsDeployment) bool {
	return deployment.ResourceID != "" &&
		deployment.Location != "" &&
		deployment.DeploymentName != ""
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func newSessionToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func assetHandler(assets fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(assets))
	index, indexErr := fs.ReadFile(assets, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)
		cleanPath := path.Clean("/" + r.URL.Path)
		if cleanPath == "/" || cleanPath == "/index.html" {
			serveIndex(w, index, indexErr)
			return
		}

		assetPath := strings.TrimPrefix(cleanPath, "/")
		if _, err := fs.Stat(assets, assetPath); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				serveIndex(w, index, indexErr)
				return
			}
			http.Error(w, "failed to inspect embedded asset", http.StatusInternalServerError)
			return
		}

		request := r.Clone(r.Context())
		assetURL := *r.URL
		assetURL.Path = "/" + assetPath
		assetURL.RawPath = ""
		request.URL = &assetURL
		fileServer.ServeHTTP(w, request)
	})
}

func serveIndex(w http.ResponseWriter, index []byte, err error) {
	if err != nil {
		http.Error(w, "index.html is missing from embedded assets", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(index)
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", strings.Join([]string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"font-src 'self' data:",
		"connect-src 'self'",
		"object-src 'none'",
		"base-uri 'none'",
		"frame-ancestors 'none'",
	}, "; "))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
}
