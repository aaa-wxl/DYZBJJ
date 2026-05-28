package redis

const placeBidScript = `
local snapshotKey = KEYS[1]
local rankKey = KEYS[2]
local amountKey = KEYS[3]
local rankSeqKey = KEYS[4]
local seqKey = KEYS[5]
local requestKey = KEYS[6]

local userID = ARGV[2]
local amount = tonumber(ARGV[4])
local nowMs = tonumber(ARGV[5])
local bidID = ARGV[6]
local requestTTLSeconds = tonumber(ARGV[7])

local existing = redis.call("GET", requestKey)
if existing then
  local decoded = cjson.decode(existing)
  decoded["idempotent"] = true
  return cjson.encode(decoded)
end

if redis.call("EXISTS", snapshotKey) == 0 then
  return cjson.encode({ok=false, error="not_found"})
end

local status = redis.call("HGET", snapshotKey, "status")
local currentPrice = tonumber(redis.call("HGET", snapshotKey, "currentPrice"))
local endsAtMs = tonumber(redis.call("HGET", snapshotKey, "endsAtUnixMs"))
local startPrice = tonumber(redis.call("HGET", snapshotKey, "startPrice"))
local increment = tonumber(redis.call("HGET", snapshotKey, "increment"))
local ceilingPrice = tonumber(redis.call("HGET", snapshotKey, "ceilingPrice"))
local extendThresholdMs = tonumber(redis.call("HGET", snapshotKey, "extendThresholdMs"))
local extendByMs = tonumber(redis.call("HGET", snapshotKey, "extendByMs"))

if status ~= "RUNNING" then
  return cjson.encode({ok=false, error="status", status=status, nextMinimum=currentPrice + increment})
end
if nowMs > endsAtMs then
  return cjson.encode({ok=false, error="expired", status=status, nextMinimum=currentPrice + increment})
end

local minimum = currentPrice + increment
if minimum < startPrice then
  minimum = startPrice
end
if amount < minimum then
  return cjson.encode({ok=false, error="low_bid", nextMinimum=minimum})
end
if ((amount - startPrice) % increment) ~= 0 then
  return cjson.encode({ok=false, error="increment", nextMinimum=minimum})
end

local seq = tonumber(redis.call("INCR", seqKey))
local previousAmount = tonumber(redis.call("HGET", amountKey, userID) or "-1")
if amount > previousAmount then
  redis.call("HSET", amountKey, userID, amount)
  redis.call("HSET", rankSeqKey, userID, seq)
  redis.call("ZADD", rankKey, amount * 1000000000 - seq, userID)
end

local extended = false
local newStatus = status
if amount >= ceilingPrice then
  newStatus = "SOLD"
elseif endsAtMs - nowMs <= extendThresholdMs and extendByMs > 0 then
  endsAtMs = endsAtMs + extendByMs
  extended = true
end

redis.call("HSET", snapshotKey,
  "status", newStatus,
  "currentPrice", amount,
  "highestBidder", userID,
  "endsAtUnixMs", endsAtMs,
  "serverTimeUnixMs", nowMs
)

local result = cjson.encode({
  ok=true,
  bidId=bidID,
  idempotent=false,
  extended=extended,
  status=newStatus,
  currentPrice=amount,
  highestBidder=userID,
  endsAtUnixMs=endsAtMs,
  serverTimeUnixMs=nowMs,
  nextMinimum=amount + increment
})
redis.call("SET", requestKey, result, "EX", requestTTLSeconds)
return result
`

const cancelScript = `
local snapshotKey = KEYS[1]
local nowMs = tonumber(ARGV[1])
if redis.call("EXISTS", snapshotKey) == 0 then
  return cjson.encode({ok=false, error="not_found"})
end
local status = redis.call("HGET", snapshotKey, "status")
if status ~= "DRAFT" and status ~= "RUNNING" then
  return cjson.encode({ok=false, error="status", status=status})
end
redis.call("HSET", snapshotKey, "status", "CANCELLED", "serverTimeUnixMs", nowMs)
return cjson.encode({ok=true})
`

const finishExpiredScript = `
local snapshotKey = KEYS[1]
local nowMs = tonumber(ARGV[1])
if redis.call("EXISTS", snapshotKey) == 0 then
  return cjson.encode({ok=false, error="not_found"})
end
local status = redis.call("HGET", snapshotKey, "status")
local endsAtMs = tonumber(redis.call("HGET", snapshotKey, "endsAtUnixMs"))
local highestBidder = redis.call("HGET", snapshotKey, "highestBidder")
if status ~= "RUNNING" then
  return cjson.encode({ok=false, error="status", status=status})
end
if nowMs < endsAtMs then
  return cjson.encode({ok=false, error="not_expired"})
end
local newStatus = "ENDED"
if highestBidder and highestBidder ~= "" then
  newStatus = "SOLD"
end
redis.call("HSET", snapshotKey, "status", newStatus, "serverTimeUnixMs", nowMs)
return cjson.encode({ok=true, status=newStatus})
`
