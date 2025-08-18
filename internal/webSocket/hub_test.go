package webSocket

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"Dexcelerate_swap_stats/internal/model"

	"github.com/gorilla/websocket"
)

func TestNewHub(t *testing.T) {
	hub := NewHub()

	if hub == nil {
		t.Fatal("NewHub() returned nil")
	}

	if hub.Subs == nil {
		t.Error("Hub.Subs is nil")
	}

	if hub.DeadCh == nil {
		t.Error("Hub.DeadCh is nil")
	}

	if len(hub.Subs) != 0 {
		t.Errorf("Expected empty Subs map, got %d entries", len(hub.Subs))
	}
}

func TestHubBroadcastEmptySubscriptions(t *testing.T) {
	hub := NewHub()

	stats := model.Stats{
		Token:     "BTC",
		UpdatedAt: time.Now(),
	}

	// Broadcasting to empty subscriptions should not panic
	hub.Broadcast("BTC", stats)
}

func TestHubBroadcastWithSubscriptions(t *testing.T) {
	hub := NewHub()

	// Create mock WebSocket connection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := hub.Upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Keep connection alive for test
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	// Convert http://127.0.0.1 to ws://127.0.0.1
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("Failed to connect to test server: %v", err)
	}
	defer conn.Close()

	// Add connection to hub
	hub.Mu.Lock()
	if hub.Subs["BTC"] == nil {
		hub.Subs["BTC"] = make(map[*websocket.Conn]struct{})
	}
	hub.Subs["BTC"][conn] = struct{}{}
	hub.Mu.Unlock()

	stats := model.Stats{
		Token:     "BTC",
		UpdatedAt: time.Now(),
	}

	// Broadcast should not panic
	hub.Broadcast("BTC", stats)

	// Allow some time for the broadcast
	time.Sleep(50 * time.Millisecond)
}

func TestHubReapDead(t *testing.T) {
	hub := NewHub()

	// Create a mock connection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := hub.Upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Don't close here, let the test handle it
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Add connection to subscriptions
	hub.Mu.Lock()
	hub.Subs["BTC"] = make(map[*websocket.Conn]struct{})
	hub.Subs["BTC"][conn] = struct{}{}
	initialCount := len(hub.Subs["BTC"])
	hub.Mu.Unlock()

	if initialCount != 1 {
		t.Errorf("Expected 1 subscription, got %d", initialCount)
	}

	// Start reaper in goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Process only one dead connection
		select {
		case deadConn := <-hub.DeadCh:
			hub.Mu.Lock()
			for _, set := range hub.Subs {
				delete(set, deadConn)
			}
			hub.Mu.Unlock()
			deadConn.Close()
		case <-time.After(2 * time.Second):
			t.Error("Timeout waiting for dead connection")
		}
	}()

	// Mark connection as dead
	hub.DeadCh <- conn

	// Wait for reaper to process
	wg.Wait()

	// Check that connection was removed
	hub.Mu.Lock()
	finalCount := len(hub.Subs["BTC"])
	hub.Mu.Unlock()

	if finalCount != 0 {
		t.Errorf("Expected 0 subscriptions after reaping, got %d", finalCount)
	}
}

func TestHubUpgraderCheckOrigin(t *testing.T) {
	hub := NewHub()

	// Create a mock request
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Origin", "http://malicious-site.com")

	// CheckOrigin should always return true based on the implementation
	result := hub.Upgrader.CheckOrigin(req)
	if !result {
		t.Error("Expected CheckOrigin to return true")
	}
}

func TestHubConcurrentAccess(t *testing.T) {
	hub := NewHub()

	var wg sync.WaitGroup
	const numGoroutines = 10

	// Test concurrent access to hub subscriptions
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			token := "BTC"
			stats := model.Stats{
				Token:     token,
				UpdatedAt: time.Now(),
			}

			// Simulate adding subscription and broadcasting
			hub.Mu.Lock()
			if hub.Subs[token] == nil {
				hub.Subs[token] = make(map[*websocket.Conn]struct{})
			}
			hub.Mu.Unlock()

			hub.Broadcast(token, stats)
		}(i)
	}

	wg.Wait()
}
