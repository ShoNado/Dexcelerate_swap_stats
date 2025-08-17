package httpApi

import (
	"encoding/json"
	"net/http"
	"time"

	"Dexcelerate_swap_stats/internal/engine"
	"Dexcelerate_swap_stats/internal/webSocket"
)

type server struct {
	engine *engine.Engine
	wsHub  *webSocket.Hub
	mux    *http.ServeMux
}

func NewServer(engine *engine.Engine, wsHub *webSocket.Hub) http.Handler {
	s := &server{
		engine: engine,
		wsHub:  wsHub,
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s.mux
}

func (s *server) routes() {
	s.mux.HandleFunc("/healthz", s.handleHealth)
	s.mux.HandleFunc("/stats", s.handleStats)
	s.mux.Handle("/ws", engine.ServeWS(s.wsHub, s.engine))
}

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "time": time.Now().UTC()})
}

func (s *server) handleStats(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "token required", http.StatusBadRequest)
		return
	}
	st := s.engine.Stats(token, time.Now())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(st)
}
