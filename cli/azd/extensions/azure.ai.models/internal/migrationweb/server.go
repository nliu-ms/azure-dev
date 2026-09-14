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
	mux.HandleFunc("GET /api/models", server.authorize(server.handleModels))
	mux.HandleFunc("GET /api/health", server.authorize(server.handleHealth))
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

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	result, err := s.provider.ListDeployments(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": fmt.Sprintf("Could not load Azure model deployments: %v", err),
		})
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
