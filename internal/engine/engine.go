package engine

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"Dexcelerate_swap_stats/internal/model"
	"Dexcelerate_swap_stats/internal/storage/redisStorage"
	"Dexcelerate_swap_stats/internal/webSocket"

	"github.com/gorilla/websocket"
)

const (
	windowMinutes = 24 * 60
)

// bucket for 24 hours for each minute
type series struct {
	StartMinute int64
	Buckets     [windowMinutes]model.Bucket
}

// Engine is in-memory store for fast answer and webSocket push
// True value stores in redisStore.Store
type Engine struct {
	mu     sync.Mutex
	series map[string]*series

	store *redisStorage.Store
	wsHub *webSocket.Hub
}
type EngineInterface interface {
	Stats(token string, now time.Time) model.Stats
	Load() error
	Apply(ev model.SwapEvent) (bool, error)
}

func NewEngine(store *redisStorage.Store, wsHub *webSocket.Hub) *Engine {
	return &Engine{
		series: make(map[string]*series),
		store:  store,
		wsHub:  wsHub,
	}
}

func unixMin(t time.Time) int64 { return t.UTC().Unix() / 60 }

func (e *Engine) Stats(token string, now time.Time) model.Stats {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, ok := e.series[token]
	if !ok { //if no information about token return empty
		return model.Stats{Token: token, UpdatedAt: time.Now().Unix()}
	}
	nowMin := unixMin(now)
	sumRange := func(minutes int64) model.Bucket {
		from := nowMin - minutes + 1
		if from < s.StartMinute {
			from = s.StartMinute
		}
		var bucket model.Bucket
		var idx int
		fmt.Println(from, nowMin)
		for m := from; m <= nowMin; m++ {
			idx = int(m-s.StartMinute) % windowMinutes
			fmt.Println(s.Buckets[idx])
			bucket.Count += s.Buckets[idx].Count
			bucket.USD += s.Buckets[idx].USD
			bucket.Quantity += s.Buckets[idx].Quantity
		}
		fmt.Println(bucket)
		return bucket
	}

	return model.Stats{
		Token:          token,
		BucketMinutes5: sumRange(1), // for testing
		BucketHours1:   sumRange(2),
		BucketHours24:  sumRange(windowMinutes),
		UpdatedAt:      time.Now().Unix(),
	}
}

// load returns data from redis to in-memory store if application restarted
func (e *Engine) Load() error {
	all, err := e.store.LoadAllSeries()
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	nowMin := time.Now().UTC().Unix() / 60
	for token, fields := range all {
		s := &series{StartMinute: nowMin - windowMinutes + 1}
		// fields: map[string]string where key like "<minute>#c" / "<minute>#u" / "<minute>#q"
		// check lua script for details
		for fname, fval := range fields {
			parts := strings.Split(fname, "#")
			if len(parts) != 2 {
				continue
			}
			minStr := parts[0]
			kind := parts[1]
			minute, err := strconv.ParseInt(minStr, 10, 64)
			if err != nil {
				continue
			}
			if minute < s.StartMinute || minute > s.StartMinute+windowMinutes-1 {
				log.Println("[err] How we got out of range minute:", minute, "for token", token)
				continue // skip out of range or IDK?
			}
			idx := int((minute - s.StartMinute) % windowMinutes)
			switch kind {
			case "c": // count
				if v, err := strconv.ParseUint(fval, 10, 64); err == nil {
					s.Buckets[idx].Count = v
				}
			case "u": // USD
				if v, err := strconv.ParseFloat(fval, 64); err == nil {
					s.Buckets[idx].USD = v
				}
			case "q": // quantity
				if v, err := strconv.ParseFloat(fval, 64); err == nil {
					s.Buckets[idx].Quantity = v
				}
			}
		}
		e.series[token] = s
	}
	return nil
}

// apply event to in-memory store and redis
// returns true if event applied and not duplicated
func (e *Engine) Apply(ev model.SwapEvent) (bool, error) {
	applied, err := e.store.ApplyEvent(ev) // apply event atomically to redis
	if err != nil {
		return false, err
	}
	if !applied {
		return false, nil
	}

	now := time.Now()
	e.mu.Lock()
	s := e.ensureSeries(ev.TokenID, now)
	e.advanceTo(s, unixMin(now))
	evMin := unixMin(ev.ExecutedAt)
	var idx int
	if evMin >= s.StartMinute {
		idx = int((evMin - s.StartMinute) % windowMinutes)
		b := s.Buckets[idx]
		b.Count++
		b.USD += ev.USD
		b.Quantity += ev.Amount
		s.Buckets[idx] = b
	}
	e.mu.Unlock()

	// Broadcast updated stats via webSocket
	//st := e.Stats(ev.TokenID, time.Now())
	//e.wsHub.Broadcast(ev.TokenID, st)
	return true, nil
}

func (e *Engine) ensureSeries(token string, now time.Time) *series {
	if s, ok := e.series[token]; ok {
		return s
	}
	start := unixMin(now) - windowMinutes + 1
	s := &series{StartMinute: start}
	e.series[token] = s
	return s
}

func (e *Engine) advanceTo(s *series, to int64) {
	curEnd := s.StartMinute + windowMinutes - 1
	if to <= curEnd {
		return
	}
	steps := to - curEnd
	if steps >= windowMinutes {
		for i := range s.Buckets {
			s.Buckets[i] = model.Bucket{}
		}
		s.StartMinute = to - windowMinutes + 1
		return
	}
	for i := int64(0); i < steps; i++ {
		idx := int((s.StartMinute + i) % windowMinutes)
		s.Buckets[idx] = model.Bucket{}
	}
	s.StartMinute += steps
}

func ServeWS(h *webSocket.Hub, eng *Engine) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "token required", http.StatusBadRequest)
			return
		}
		conn, err := h.Upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		h.Mu.Lock()
		set := h.Subs[token]
		if set == nil {
			set = make(map[*websocket.Conn]struct{})
			h.Subs[token] = set
		}
		set[conn] = struct{}{}
		h.Mu.Unlock()

		// initial snapshot
		_ = conn.WriteJSON(eng.Stats(token, time.Now()))

		// reader to detect close
		go func(c *websocket.Conn) {
			defer func() { h.DeadCh <- c }()
			c.SetReadLimit(512)
			err := c.SetReadDeadline(time.Now().Add(60 * time.Second))
			if err != nil {
				log.Println("[error] Failed to set read deadline:", err)
				return
			}
			c.SetPongHandler(func(string) error {
				err = c.SetReadDeadline(time.Now().Add(60 * time.Second))
				if err != nil {
					log.Println("[error] Failed to set read deadline:", err)
					return err
				}
				return nil
			})
			for {
				if _, _, err := c.ReadMessage(); err != nil {
					return
				}
			}
		}(conn)
	})
}
