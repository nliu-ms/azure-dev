// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"path"
	"strings"
	"time"
)

type ServerOptions struct {
	Port           int
	SubscriptionID string
	Provider       ModelProvider
}

type Server struct {
	listener       net.Listener
	httpServer     *http.Server
	subscriptionID string
	provider       ModelProvider
	token          string
}

const inventoryRequestTimeout = 60 * time.Second
const resourceRequestTimeout = 20 * time.Second

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
		listener:       listener,
		subscriptionID: options.SubscriptionID,
		provider:       options.Provider,
		token:          token,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/resources", server.authorize(server.handleResources))
	mux.HandleFunc("POST /api/resource-models", server.authorize(server.handleResourceModels))
	mux.HandleFunc("GET /api/models", server.authorize(server.handleModels))
	mux.HandleFunc("GET /api/health", server.authorize(server.handleHealth))
	mux.HandleFunc("POST /api/recommendations", server.authorize(server.handleRecommendation))
	mux.HandleFunc("POST /api/assessments", server.authorize(server.handleAssessment))
	mux.HandleFunc("POST /api/deployment-options", server.authorize(server.handleDeploymentOptions))
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
