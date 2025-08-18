package redisStorage

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"Dexcelerate_swap_stats/internal/model"

	"github.com/redis/go-redis/v9"

	_ "embed"
)

// get embed package to include Lua script for atomic event processing
//
//go:embed lua/applyEvent.lua
var LuaScript string

type Store struct {
	cli          *redis.Client
	dedupleTTL   int64
	tokensKey    string
	ctx          context.Context
	script       string
	eventCounter int64
	lastEventKey string
}

func NewStore(cli *redis.Client, tokensKey string, dedupleTTL time.Duration) *Store {
	return &Store{
		cli:          cli,
		dedupleTTL:   int64(dedupleTTL.Seconds()),
		tokensKey:    tokensKey,
		ctx:          context.Background(),
		script:       LuaScript,
		eventCounter: 0,
		lastEventKey: "lastEventID",
	}
}

// ApplyEvent processes a swap event atomically using a Lua script,
// returns true if applied and not duplicated
func (s *Store) ApplyEvent(ev model.SwapEvent) (bool, error) {
	// Prepare keys and values for Lua script execution
	dedupeKey := "dedupe:" + ev.EventID
	seriesKey := "series:" + ev.TokenID
	tokenSet := s.tokensKey
	minute := strconv.FormatInt(ev.ExecutedAt.UTC().Unix()/60, 10)
	usdStr := strconv.FormatFloat(ev.USD, 'f', -1, 64)
	quantityStr := strconv.FormatFloat(ev.Amount, 'f', -1, 64)
	ttlStr := strconv.FormatInt(s.dedupleTTL, 10)

	res, err := s.cli.Eval(s.ctx, s.script, []string{dedupeKey, seriesKey, tokenSet},
		ev.EventID, minute, usdStr, quantityStr, ttlStr, ev.TokenID).Result()
	if err != nil {
		return false, err
	}

	var applied bool
	switch v := res.(type) {
	case int64:
		applied = v == 1
	case string:
		// some Redis libs return string
		applied = v == "1"
	default:
		return false, fmt.Errorf("unexpected result from redis: %v", res)
	}

	if applied {
		s.eventCounter++
		if s.eventCounter%100 == 0 {
			if err := s.setLastEventID(ev.EventID); err != nil {
				log.Printf("[warning] Failed to set lastEventID: %v", err)
			}
		}
	}

	return applied, nil
}

func (s *Store) setLastEventID(eventID string) error {
	return s.cli.Set(s.ctx, s.lastEventKey, eventID, 0).Err()
}

func (s *Store) GetLastEventID() (string, error) {
	result, err := s.cli.Get(s.ctx, s.lastEventKey).Result()
	if err == redis.Nil {
		return "", nil
	}
	return result, err
}

// GetEventCounter returns current event counter value
func (s *Store) GetEventCounter() int64 {
	return s.eventCounter
}

// SetEventCounter sets event counter value (useful for initialization after restart)
func (s *Store) SetEventCounter(counter int64) {
	s.eventCounter = counter
}

func (s *Store) LoadAllSeries() (map[string]map[string]string, error) {
	out := make(map[string]map[string]string)

	//read all tokens
	tokens, err := s.cli.SMembers(s.ctx, s.tokensKey).Result()
	if err != nil {
		return nil, err
	}
	for _, token := range tokens {
		key := "series:" + token
		fields, err := s.cli.HGetAll(s.ctx, key).Result()
		if err != nil {
			log.Printf("Failed to get key %s: %v", key, err)
			//or we can return with error and stop processing
		}
		out[token] = fields
	}
	return out, nil
}
