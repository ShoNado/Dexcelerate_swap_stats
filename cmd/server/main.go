package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"Dexcelerate_swap_stats/internal/config"
	"Dexcelerate_swap_stats/internal/engine"
	"Dexcelerate_swap_stats/internal/httpApi"
	"Dexcelerate_swap_stats/internal/model"
	"Dexcelerate_swap_stats/internal/storage/redisStorage"
	"Dexcelerate_swap_stats/internal/webSocket"

	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()

	// start redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisURL,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatal("[fatal err] Can't start redis:", err)
	}

	// initialize all parts
	wsHub := webSocket.NewHub()
	store := redisStorage.NewStore(rdb, "token_set", cfg.DedupeTTL)
	eng := engine.NewEngine(store, wsHub)

	//try to load data from redis
	if err := eng.Load(); err != nil {
		log.Println("[boot] Error loading data from redis:", err)
	} else {
		log.Println("[boot] Data loaded from redis")
	}

	// consumer loop to simulate reading from kafka
	events := make(chan model.SwapEvent, 8192) // buffer size 2^13
	log.Println("[boot] Starting demo producer")
	ctx, cancel := context.WithCancel(context.Background())
	go DemoProducer(ctx, events)

	//main loop
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-events:
				applied, err := eng.Apply(ev)
				if err != nil {
					log.Println("[error] Failed to apply event:", err)
				} else if !applied {
					log.Println("[error] Event is duplicate, not applied:", ev.EventID)
				}
			}
		}
	}()

	// start webSocket reaper
	go wsHub.ReapDead()

	//start http server
	server := httpApi.NewServer(eng, wsHub)
	srv := &http.Server{
		Addr:         cfg.HttpAddr,
		Handler:      server,
		WriteTimeout: 5 * time.Second,
		ReadTimeout:  30 * time.Second,
	}
	go func() {
		log.Println("[boot] Starting http server on", cfg.HttpAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("[fatal err] HTTP server failed:", err)
		}
	}()

	//graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("[shutdown] Shutting down")
	cancel()
	log.Println("[shutdown] Shutdown complete")
}

// DemoProducer generates swap events and sends them to the provided channel at a fixed interval using a ticker.
func DemoProducer(_ context.Context, out chan model.SwapEvent) {
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()

	id := 0
	tokens := []string{"ETH", "BTC", "SOL"}
	var ev model.SwapEvent

	for now := range t.C {
		id++
		ev = model.SwapEvent{
			EventID:    "ev:" + now.Format(time.RFC3339Nano) + ":" + []string{"a", "b", "c", "d", "f"}[id%5],
			TokenID:    tokens[id%len(tokens)],
			Amount:     1.0,
			USD:        1111.1 + float64(id%100),
			Side:       model.Sides[id%2],
			Rate:       2222.2 + float64(id%100),
			CreatedAt:  now.Add(-time.Second * time.Duration(id%100)),
			ExecutedAt: now,
		}
		out <- ev

		//simulate duplicates
		/*if id%123 == 0 {
			out <- ev
		}*/
	}
}
