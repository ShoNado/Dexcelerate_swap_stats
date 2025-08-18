package httpApi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"Dexcelerate_swap_stats/internal/model"
	"Dexcelerate_swap_stats/internal/webSocket"
)

// Mock engine for testing
type mockEngine struct {
	statsData map[string]model.Stats
}

func newMockEngine() *mockEngine {
	return &mockEngine{
		statsData: make(map[string]model.Stats),
	}
}

func (m *mockEngine) Stats(token string, now time.Time) model.Stats {
	if stats, exists := m.statsData[token]; exists {
		return stats
	}
	return model.Stats{
		Token:     token,
		UpdatedAt: now,
	}
}

func (m *mockEngine) Load() error {
	return nil
}

func (m *mockEngine) Apply(ev model.SwapEvent) (bool, error) {
	return true, nil
}

func (m *mockEngine) StartPeriodicUpdates() {
	// Mock implementation
}

func TestNewServer(t *testing.T) {
	mockEng := newMockEngine()
	hub := webSocket.NewHub()

	server := NewServer(mockEng, hub)

	if server == nil {
		t.Fatal("NewServer() returned nil")
	}
}

func TestHealthHandler(t *testing.T) {
	mockEng := newMockEngine()
	hub := webSocket.NewHub()
	server := NewServer(mockEng, hub)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to parse response JSON: %v", err)
	}

	if ok, exists := response["ok"]; !exists || ok != true {
		t.Errorf("Expected ok=true in response, got %v", ok)
	}

	if _, exists := response["time"]; !exists {
		t.Error("Expected time field in response")
	}
}

func TestStatsHandlerSuccess(t *testing.T) {
	mockEng := newMockEngine()
	hub := webSocket.NewHub()

	// Prepare mock data
	expectedStats := model.Stats{
		Token: "BTC",
		BucketMinutes5: model.Bucket{
			Count:    10,
			USD:      1000.0,
			Quantity: 0.1,
		},
		BucketHours1: model.Bucket{
			Count:    50,
			USD:      5000.0,
			Quantity: 0.5,
		},
		BucketHours24: model.Bucket{
			Count:    500,
			USD:      50000.0,
			Quantity: 5.0,
		},
		UpdatedAt: time.Now(),
	}

	mockEng.statsData["BTC"] = expectedStats

	server := NewServer(mockEng, hub)

	req := httptest.NewRequest("GET", "/stats?token=BTC", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	var response model.Stats
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to parse response JSON: %v", err)
	}

	if response.Token != expectedStats.Token {
		t.Errorf("Expected token %s, got %s", expectedStats.Token, response.Token)
	}

	if response.BucketMinutes5.Count != expectedStats.BucketMinutes5.Count {
		t.Errorf("Expected BucketMinutes5.Count %d, got %d",
			expectedStats.BucketMinutes5.Count, response.BucketMinutes5.Count)
	}
}

func TestStatsHandlerMissingToken(t *testing.T) {
	mockEng := newMockEngine()
	hub := webSocket.NewHub()
	server := NewServer(mockEng, hub)

	req := httptest.NewRequest("GET", "/stats", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	body := strings.TrimSpace(w.Body.String())
	if body != "token required" {
		t.Errorf("Expected 'token required', got '%s'", body)
	}
}

func TestStatsHandlerEmptyToken(t *testing.T) {
	mockEng := newMockEngine()
	hub := webSocket.NewHub()
	server := NewServer(mockEng, hub)

	req := httptest.NewRequest("GET", "/stats?token=", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestStatsHandlerUnknownToken(t *testing.T) {
	mockEng := newMockEngine()
	hub := webSocket.NewHub()
	server := NewServer(mockEng, hub)

	req := httptest.NewRequest("GET", "/stats?token=UNKNOWN", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response model.Stats
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to parse response JSON: %v", err)
	}

	if response.Token != "UNKNOWN" {
		t.Errorf("Expected token UNKNOWN, got %s", response.Token)
	}

	// Should return empty stats for unknown token
	if response.BucketMinutes5.Count != 0 {
		t.Errorf("Expected empty stats for unknown token, got %d transactions", response.BucketMinutes5.Count)
	}
}

func TestInvalidRoute(t *testing.T) {
	mockEng := newMockEngine()
	hub := webSocket.NewHub()
	server := NewServer(mockEng, hub)

	req := httptest.NewRequest("GET", "/invalid", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}
