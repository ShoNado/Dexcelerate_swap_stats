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
	cli        *redis.Client
	dedupleTTL int64
	tokensKey  string
	ctx        context.Context
	script     string
}

func NewStore(cli *redis.Client, tokensKey string, dedupleTTL time.Duration) *Store {
	return &Store{
		cli:        cli,
		dedupleTTL: int64(dedupleTTL.Seconds()), //
		tokensKey:  tokensKey,
		ctx:        context.Background(),
		script:     LuaScript,
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
		ev.EventID, minute, usdStr, quantityStr, ttlStr).Result()
	if err != nil {
		return false, err
	}

	switch v := res.(type) {
	case int64:
		return v == 1, nil
	case string:
		// some Redis libs return string
		if v == "1" {
			return true, nil
		}
	}
	return false, fmt.Errorf("unepected result from redis: %v", res)
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
		fileds, err := s.cli.HGetAll(s.ctx, key).Result()
		if err != nil {
			log.Printf("Failed to get key %s: %v", key, err)
			//or we can return with error and stop processing
		}
		out[token] = fileds
	}
	return out, nil
}
