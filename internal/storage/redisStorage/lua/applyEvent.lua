-- KEYS: dedupeKey, seriesKey, tokensSet
-- ARGV:  minute, usd, qty, ttlSeconds, token
local dedupeKey = KEYS[1]
local seriesKey = KEYS[2]
local tokensSet = KEYS[3]

local _ = ARGV[1] --eventID
local minute = ARGV[2]
local usd    = ARGV[3]
local qty    = ARGV[4]
local ttl    = tonumber(ARGV[5])

-- check for duplicate event
if redis.call("EXISTS", dedupeKey) == 1 then
  return 0
end

if ttl and ttl > 0 then
  redis.call("SET", dedupeKey, 1, "EX", ttl)
else
  redis.call("SET", dedupeKey, 1)
end

-- incrementing data in buckets (count/usd/qty)
redis.call("HINCRBY",       seriesKey, minute .. "#c", 1)
redis.call("HINCRBYFLOAT",  seriesKey, minute .. "#u", usd)
redis.call("HINCRBYFLOAT",  seriesKey, minute .. "#q", qty)

-- adding token to set
-- this is used to prevent duplicate events in the same minute
redis.call("SADD", tokensSet, ARGV[6])

return 1