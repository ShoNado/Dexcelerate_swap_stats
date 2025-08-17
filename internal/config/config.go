package config

import (
	"log"
	"os"
	"strconv"
	"time"
)

type Config struct {
	RedisURL      string
	RedisPassword string
	RedisDB       int
	HttpAddr      string
	DedupeTTL     time.Duration
	Debug         bool
}

// GetConfig default values for using locally
func GetConfig() Config {
	return Config{
		RedisURL:      getEnv("REDIS_URL", "localhost:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       mustAtoi(getEnv("REDIS_DB", "0")),
		HttpAddr:      getEnv("HTTP_ADDR", ":8080"),
		DedupeTTL:     parseDuration(getEnv("DEDUPE_TTL", "25h")),
		Debug:         getEnvBool("DEBUG", false),
	}

}

func getEnv(key, defaultValue string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return defaultValue
}

func mustAtoi(s string) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		log.Fatal(err)
	}
	return v
}

func getEnvBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Fatal(err)
	}
	return d
}
