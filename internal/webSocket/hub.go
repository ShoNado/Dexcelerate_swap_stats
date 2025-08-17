package webSocket

import (
	"net/http"
	"sync"

	"Dexcelerate_swap_stats/internal/model"

	"github.com/gorilla/websocket"
)

type Hub struct {
	Upgrader websocket.Upgrader

	Mu     sync.Mutex
	Subs   map[string]map[*websocket.Conn]struct{}
	DeadCh chan *websocket.Conn
}

func NewHub() *Hub {
	return &Hub{
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		Subs:   make(map[string]map[*websocket.Conn]struct{}),
		DeadCh: make(chan *websocket.Conn, 1024),
	}
}

func (h *Hub) Broadcast(token string, st model.Stats) {
	h.Mu.Lock()
	set := h.Subs[token]
	h.Mu.Unlock()
	if len(set) == 0 {
		return
	}
	for c := range set {
		_ = c.WriteJSON(st)
	}
}

func (h *Hub) ReapDead() {
	for c := range h.DeadCh {
		h.Mu.Lock()
		for _, set := range h.Subs {
			delete(set, c)
		}
		h.Mu.Unlock()
		_ = c.Close()
	}
}
