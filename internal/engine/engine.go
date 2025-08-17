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
	Token       string
	StartMinute int64
	Buckets     []model.Bucket
}

// Engine is an in-memory store for fast answer and webSocket push
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

		for m := from; m <= nowMin; m++ {
			idx = int(m-s.StartMinute) % windowMinutes
			bucket.Count += s.Buckets[idx].Count
			bucket.USD += s.Buckets[idx].USD
			bucket.Quantity += s.Buckets[idx].Quantity
		}

		return bucket
	}

	return model.Stats{
		Token:          token,
		BucketMinutes5: sumRange(5),
		BucketHours1:   sumRange(60),
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

	nowMin := unixMin(time.Now().UTC())
	start := nowMin - int64(windowMinutes) + 1
	end := nowMin

	e.mu.Lock()
	defer e.mu.Unlock()

	e.series = make(map[string]*series, len(all))

	for token, fields := range all {
		s := &series{StartMinute: start}
		// buckets нулями по умолчанию

		for fname, raw := range fields {
			// ожидаем формат "<minute>#<kind>", где kind ∈ {c,u,q}
			parts := strings.Split(fname, "#")
			if len(parts) != 2 {
				continue
			}
			minute, err := strconv.ParseInt(parts[0], 10, 64)
			if err != nil {
				continue
			}
			kind := parts[1]

			// игнорируем всё вне нашего окна [start..end]
			if minute < start || minute > end {
				continue
			}

			idx := int(minute - s.StartMinute)
			if idx < 0 || idx >= windowMinutes {
				continue
			}

			switch kind {
			case "c":
				if v, err := strconv.ParseUint(raw, 10, 64); err == nil {
					s.Buckets[idx].Count = v
				}
			case "u":
				if v, err := strconv.ParseFloat(raw, 64); err == nil {
					s.Buckets[idx].USD = v
				}
			case "q":
				if v, err := strconv.ParseFloat(raw, 64); err == nil {
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
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now().UTC()
	nowMin := unixMin(now)

	evMin := unixMin(ev.ExecutedAt.UTC())
	if evMin > nowMin {
		evMin = nowMin
	}

	applied, err := e.store.ApplyEvent(ev) // apply event atomically to redis
	if err != nil {
		return false, err
	}
	if !applied {
		return false, nil
	}

	s := e.ensureSeries(ev.TokenID, now)
	e.advanceTo(s, nowMin)
	if evMin < s.StartMinute {
		//too ald event > 24hrs
		return false, fmt.Errorf("invalid start minute: %d", evMin)
	}
	if evMin > nowMin {
		//event from future
		return false, fmt.Errorf("invalid end minute: %d", evMin)
	}
	idx := int(evMin - s.StartMinute)
	if idx < 0 || idx >= len(s.Buckets) {
		//out of window
		return false, fmt.Errorf("invalid index: %d", idx)
	}
	s.Buckets[idx].Count++
	s.Buckets[idx].USD += ev.USD
	s.Buckets[idx].Quantity += ev.Amount

	// Broadcast updated stats via webSocket
	go func() {
		st := e.Stats(ev.TokenID, time.Now())
		e.wsHub.Broadcast(ev.TokenID, st)
	}()
	return true, nil
}

func (e *Engine) ensureSeries(token string, now time.Time) *series {
	if s, ok := e.series[token]; ok {
		return s
	}
	s := &series{
		Token:       token,
		StartMinute: unixMin(now) - windowMinutes + 1,
		Buckets:     make([]model.Bucket, windowMinutes),
	}
	e.series[token] = s
	return s
}

func (e *Engine) advanceTo(s *series, nowMin int64) {
	curEnd := s.StartMinute + windowMinutes - 1
	if nowMin <= curEnd {
		return // window already covers cur minute
	}
	steps := nowMin - curEnd
	if steps >= windowMinutes {
		for i := range s.Buckets {
			s.Buckets[i] = model.Bucket{}
		}
		s.StartMinute = nowMin - windowMinutes + 1
		return
	}

	offset := steps
	if offset >= 0 {
		copy(s.Buckets, s.Buckets[offset:])
		for i := windowMinutes - offset; i < windowMinutes; i++ {
			s.Buckets[i] = model.Bucket{}
		}
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
