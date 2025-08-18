package engine

import (
	"testing"
	"time"

	"Dexcelerate_swap_stats/internal/model"
	"Dexcelerate_swap_stats/internal/webSocket"

	_ "github.com/redis/go-redis/v9"
)

// Mock storage for testing
type mockStorage struct {
	events    map[string]bool // eventID -> applied
	lastEvent string
	counter   int64
	series    map[string]map[string]string
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		events: make(map[string]bool),
		series: make(map[string]map[string]string),
	}
}

func (m *mockStorage) ApplyEvent(ev model.SwapEvent) (bool, error) {
	if m.events[ev.EventID] {
		return false, nil // duplicate
	}
	m.events[ev.EventID] = true
	m.counter++

	// Simulate storing in series format
	token := ev.TokenID
	if m.series[token] == nil {
		m.series[token] = make(map[string]string)
	}

	minute := ev.ExecutedAt.UTC().Unix() / 60
	key := string(rune(minute))

	m.series[token][key+"#c"] = "1"
	m.series[token][key+"#u"] = "100.0"
	m.series[token][key+"#q"] = "1.0"

	return true, nil
}

func (m *mockStorage) LoadAllSeries() (map[string]map[string]string, error) {
	return m.series, nil
}

func (m *mockStorage) GetLastEventID() (string, error) {
	return m.lastEvent, nil
}

func (m *mockStorage) GetEventCounter() int64 {
	return m.counter
}

func (m *mockStorage) SetEventCounter(counter int64) {
	m.counter = counter
}

func TestNewEngine(t *testing.T) {
	store := newMockStorage()
	hub := webSocket.NewHub()

	engine := NewEngine(store, hub)

	if engine == nil {
		t.Fatal("NewEngine() returned nil")
	}

	if engine.series == nil {
		t.Error("Engine.series is nil")
	}

	if len(engine.series) != 0 {
		t.Errorf("Expected empty series map, got %d entries", len(engine.series))
	}
}

func TestEngineStatsEmptyToken(t *testing.T) {
	store := newMockStorage()
	hub := webSocket.NewHub()
	engine := NewEngine(store, hub)

	now := time.Now()
	stats := engine.Stats("BTC", now)

	if stats.Token != "BTC" {
		t.Errorf("Expected token BTC, got %s", stats.Token)
	}

	// Should return empty stats for non-existent token
	if stats.BucketMinutes5.Count != 0 {
		t.Errorf("Expected 0 transactions, got %d", stats.BucketMinutes5.Count)
	}
	if stats.BucketMinutes5.USD != 0 {
		t.Errorf("Expected 0 USD, got %f", stats.BucketMinutes5.USD)
	}
	if stats.BucketMinutes5.Quantity != 0 {
		t.Errorf("Expected 0 quantity, got %f", stats.BucketMinutes5.Quantity)
	}
}

func TestEngineApplyEvent(t *testing.T) {
	store := newMockStorage()
	hub := webSocket.NewHub()
	engine := NewEngine(store, hub)

	now := time.Now()
	event := model.SwapEvent{
		EventID:    "test-event-1",
		TokenID:    "BTC",
		Amount:     1.0,
		USD:        50000.0,
		Side:       model.Buy,
		Rate:       50000.0,
		CreatedAt:  now.Add(-time.Minute),
		ExecutedAt: now,
	}

	applied, err := engine.Apply(event)

	if err != nil {
		t.Fatalf("Apply() returned error: %v", err)
	}

	if !applied {
		t.Error("Expected event to be applied")
	}

	// Test duplicate
	applied, err = engine.Apply(event)

	if err != nil {
		t.Fatalf("Apply() returned error on duplicate: %v", err)
	}

	if applied {
		t.Error("Expected duplicate event to not be applied")
	}
}

func TestEngineApplyAndStats(t *testing.T) {
	store := newMockStorage()
	hub := webSocket.NewHub()
	engine := NewEngine(store, hub)

	now := time.Now()
	event := model.SwapEvent{
		EventID:    "test-event-1",
		TokenID:    "BTC",
		Amount:     1.0,
		USD:        50000.0,
		Side:       model.Buy,
		Rate:       50000.0,
		CreatedAt:  now.Add(-time.Minute),
		ExecutedAt: now,
	}

	applied, err := engine.Apply(event)
	if err != nil {
		t.Fatalf("Apply() returned error: %v", err)
	}
	if !applied {
		t.Fatal("Expected event to be applied")
	}

	// Get stats
	stats := engine.Stats("BTC", now.Add(time.Minute))

	if stats.Token != "BTC" {
		t.Errorf("Expected token BTC, got %s", stats.Token)
	}

	// Check 5-minute bucket includes the event
	if stats.BucketMinutes5.Count == 0 {
		t.Error("Expected non-zero count in 5-minute bucket")
	}

	if stats.BucketMinutes5.USD == 0 {
		t.Error("Expected non-zero USD in 5-minute bucket")
	}

	if stats.BucketMinutes5.Quantity == 0 {
		t.Error("Expected non-zero quantity in 5-minute bucket")
	}
}

func TestEngineMultipleEvents(t *testing.T) {
	store := newMockStorage()
	hub := webSocket.NewHub()
	engine := NewEngine(store, hub)

	now := time.Now()
	events := []model.SwapEvent{
		{
			EventID:    "event-1",
			TokenID:    "BTC",
			Amount:     1.0,
			USD:        50000.0,
			Side:       model.Buy,
			Rate:       50000.0,
			ExecutedAt: now,
		},
		{
			EventID:    "event-2",
			TokenID:    "BTC",
			Amount:     0.5,
			USD:        25000.0,
			Side:       model.Sell,
			Rate:       50000.0,
			ExecutedAt: now,
		},
		{
			EventID:    "event-3",
			TokenID:    "ETH",
			Amount:     10.0,
			USD:        30000.0,
			Side:       model.Buy,
			Rate:       3000.0,
			ExecutedAt: now,
		},
	}

	for _, event := range events {
		applied, err := engine.Apply(event)
		if err != nil {
			t.Fatalf("Apply() returned error for %s: %v", event.EventID, err)
		}
		if !applied {
			t.Errorf("Expected event %s to be applied", event.EventID)
		}
	}

	// Check BTC stats
	btcStats := engine.Stats("BTC", now.Add(time.Minute))
	if btcStats.BucketMinutes5.Count != 2 {
		t.Errorf("Expected 2 BTC transactions, got %d", btcStats.BucketMinutes5.Count)
	}

	expectedBTCUSD := 75000.0 // 50000 + 25000
	if btcStats.BucketMinutes5.USD != expectedBTCUSD {
		t.Errorf("Expected BTC USD %f, got %f", expectedBTCUSD, btcStats.BucketMinutes5.USD)
	}

	expectedBTCQty := 1.5 // 1.0 + 0.5
	if btcStats.BucketMinutes5.Quantity != expectedBTCQty {
		t.Errorf("Expected BTC quantity %f, got %f", expectedBTCQty, btcStats.BucketMinutes5.Quantity)
	}

	// Check ETH stats
	ethStats := engine.Stats("ETH", now.Add(time.Minute))
	if ethStats.BucketMinutes5.Count != 1 {
		t.Errorf("Expected 1 ETH transaction, got %d", ethStats.BucketMinutes5.Count)
	}
}

func TestEngineOldEvent(t *testing.T) {
	store := newMockStorage()
	hub := webSocket.NewHub()
	engine := NewEngine(store, hub)

	now := time.Now()
	oldEvent := model.SwapEvent{
		EventID:    "old-event",
		TokenID:    "BTC",
		Amount:     1.0,
		USD:        50000.0,
		Side:       model.Buy,
		Rate:       50000.0,
		ExecutedAt: now.Add(-25 * time.Hour), // Too old (>24h)
	}

	applied, err := engine.Apply(oldEvent)

	// Should return error for events older than 24 hours
	if err == nil {
		t.Error("Expected error for old event")
	}

	if applied {
		t.Error("Expected old event to not be applied")
	}
}

func TestEngineLoad(t *testing.T) {
	store := newMockStorage()
	hub := webSocket.NewHub()
	engine := NewEngine(store, hub)

	// Prepare mock data
	store.series["BTC"] = map[string]string{
		"1000#c": "10",
		"1000#u": "500000.0",
		"1000#q": "10.0",
	}

	err := engine.Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	// Verify data was loaded
	if len(engine.series) == 0 {
		t.Error("Expected series to be loaded")
	}
}

func TestUnixMinFunction(t *testing.T) {
	testTime := time.Date(2023, 1, 1, 12, 30, 45, 0, time.UTC)
	expected := testTime.Unix() / 60

	result := unixMin(testTime)

	if result != expected {
		t.Errorf("Expected %d, got %d", expected, result)
	}
}

func TestEngineEnsureSeries(t *testing.T) {
	store := newMockStorage()
	hub := webSocket.NewHub()
	engine := NewEngine(store, hub)

	now := time.Now()

	// First call should create new series
	series1 := engine.ensureSeries("BTC", now)
	if series1 == nil {
		t.Fatal("ensureSeries returned nil")
	}
	if series1.Token != "BTC" {
		t.Errorf("Expected token BTC, got %s", series1.Token)
	}

	// Second call should return same series
	series2 := engine.ensureSeries("BTC", now)
	if series1 != series2 {
		t.Error("ensureSeries should return same series for same token")
	}
}

// Benchmark tests
func BenchmarkEngineApply(b *testing.B) {
	store := newMockStorage()
	hub := webSocket.NewHub()
	engine := NewEngine(store, hub)

	now := time.Now()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event := model.SwapEvent{
			EventID:    string(rune(i)), // Simple unique ID
			TokenID:    "BTC",
			Amount:     1.0,
			USD:        50000.0,
			Side:       model.Buy,
			Rate:       50000.0,
			ExecutedAt: now,
		}

		_, _ = engine.Apply(event)
	}
}

func BenchmarkEngineStats(b *testing.B) {
	store := newMockStorage()
	hub := webSocket.NewHub()
	engine := NewEngine(store, hub)

	// Apply some events first
	now := time.Now()
	for i := 0; i < 100; i++ {
		event := model.SwapEvent{
			EventID:    string(rune(i)),
			TokenID:    "BTC",
			Amount:     1.0,
			USD:        50000.0,
			Side:       model.Buy,
			Rate:       50000.0,
			ExecutedAt: now,
		}
		_, _ = engine.Apply(event)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = engine.Stats("BTC", now)
	}
}
