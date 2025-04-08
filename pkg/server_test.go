package pkg

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mockHostContext struct {
	host    *Host
	facts   Facts
	history History
}

func (m *mockHostContext) GetHost() *Host {
	return m.host
}

func (m *mockHostContext) GetFacts() Facts {
	return m.facts
}

func (m *mockHostContext) GetHistory() History {
	return m.history
}

func (m *mockHostContext) RunCommand(command, username string) (string, string, error) {
	return "", "", nil
}

func (m *mockHostContext) ReadFile(filename, username string) (string, error) {
	return "", nil
}

func (m *mockHostContext) WriteFile(filename, contents, username string) error {
	return nil
}

func TestServer_Health(t *testing.T) {
	// Create a new server instance
	server := NewServer("8080")

	// Create a new HTTP recorder
	w := httptest.NewRecorder()

	// Create a new request
	req, err := http.NewRequest("GET", "/health", nil)
	assert.NoError(t, err)

	// Serve the request
	server.router.ServeHTTP(w, req)

	// Assert the response
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))

	var response map[string]string
	err = json.NewDecoder(w.Body).Decode(&response)
	assert.NoError(t, err)
	assert.Equal(t, "ok", response["status"])
}

func TestServer_Metrics(t *testing.T) {
	// Create a new server instance
	server := NewServer("8080")

	// Create a new HTTP recorder
	w := httptest.NewRecorder()

	// Create a new request
	req, err := http.NewRequest("GET", "/metrics", nil)
	assert.NoError(t, err)

	// Serve the request
	server.router.ServeHTTP(w, req)

	// Assert the response
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
}

func TestServer_Tasks(t *testing.T) {
	// Create a new server instance
	server := NewServer("8080")

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "List Tasks",
			method:     "GET",
			path:       "/api/tasks",
			wantStatus: http.StatusOK,
			wantBody:   "[]", // Empty array as default
		},
		{
			name:       "Get Task",
			method:     "GET",
			path:       "/api/tasks/123",
			wantStatus: http.StatusNotFound, // Assuming task doesn't exist
		},
		{
			name:       "Create Task",
			method:     "POST",
			path:       "/api/tasks",
			body:       `{"name": "test-task"}`,
			wantStatus: http.StatusCreated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, err := http.NewRequest(tt.method, tt.path, nil)
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			assert.NoError(t, err)

			server.router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.wantBody != "" {
				assert.JSONEq(t, tt.wantBody, w.Body.String())
			}
		})
	}
}
