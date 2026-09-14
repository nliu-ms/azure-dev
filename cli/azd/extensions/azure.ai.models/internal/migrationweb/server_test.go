// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type staticProvider struct {
	result ModelList
	err    error
}

func (p staticProvider) ListDeployments(context.Context) (ModelList, error) {
	return p.result, p.err
}

func TestServerServesAssetsAndProtectsAPI(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	server, err := NewServer(ServerOptions{
		SubscriptionID: "sub",
		Provider: staticProvider{result: ModelList{
			SubscriptionID: "sub",
			GeneratedAt:    time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC),
			Models: []ModelDeployment{{
				DeploymentName: "chat",
				ModelName:      "gpt-4o",
				ModelVersion:   "2024-05-13",
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx)
	}()

	pageResponse, err := http.Get(server.BrowserURL())
	if err != nil {
		t.Fatal(err)
	}
	page, err := io.ReadAll(pageResponse.Body)
	_ = pageResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if pageResponse.StatusCode != http.StatusOK || !strings.Contains(string(page), "Model Migration") {
		t.Fatalf("unexpected page response: status=%d body=%q", pageResponse.StatusCode, string(page))
	}

	unauthorized, err := http.Get(server.URL() + "/api/models")
	if err != nil {
		t.Fatal(err)
	}
	_ = unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unexpected unauthorized status: %d", unauthorized.StatusCode)
	}

	browserURL, err := url.Parse(server.BrowserURL())
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL()+"/api/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+browserURL.Query().Get("token"))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected API status: %d", response.StatusCode)
	}
	var payload ModelList
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Models) != 1 || payload.Models[0].DeploymentName != "chat" {
		t.Fatalf("unexpected API payload: %+v", payload)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
