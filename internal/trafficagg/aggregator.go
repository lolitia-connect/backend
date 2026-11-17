package trafficagg

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/tasklock"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const MaxReportedTraffic int64 = 10 << 40

const (
	bucketIndex      = "traffic:agg:buckets"
	bucketPrefix     = "traffic:agg:"
	processingPrefix = "traffic:processing:"
	processedPrefix  = "traffic:processed:"
	failurePrefix    = "traffic:failed:"
	deadPrefix       = "traffic:deadletter:"
	deadIndex        = "traffic:deadletter:buckets"
	serverReports    = "traffic:server:last_reported"
	bucketTTL        = 24 * time.Hour
	deadTTL          = 30 * 24 * time.Hour
)

type Report struct {
	SID      int64
	Upload   int64
	Download int64
}

type Aggregator struct {
	Store      repository.Store
	Redis      *redis.Client
	Multiplier func(time.Time) float32
	Served     func(context.Context, int64, string, string) (map[int64]struct{}, error)
}

func (aggregator *Aggregator) AddReportAt(ctx context.Context, server *node.Server, protocol, protocolID string, logs []Report, now time.Time) error {
	return aggregator.AddReportOnceAt(ctx, server, protocol, protocolID, logs, now, "")
}

func (aggregator *Aggregator) AddReportOnceAt(ctx context.Context, server *node.Server, protocol, protocolID string, logs []Report, now time.Time, reportID string) error {
	if aggregator.Redis == nil || server == nil || server.Id <= 0 {
		return errors.New("traffic aggregator is not initialized")
	}
	protocols, err := server.UnmarshalProtocols()
	if err != nil {
		return err
	}
	ratio, found := float64(1), false
	for _, candidate := range protocols {
		if candidate.Type == protocol && candidate.Id == protocolID {
			found = true
			if candidate.Ratio > 0 {
				ratio = candidate.Ratio
			}
			break
		}
	}
	if !found {
		return fmt.Errorf("protocol instance %s:%s not found", protocol, protocolID)
	}
	factor := ratio
	if aggregator.Multiplier != nil {
		factor *= float64(aggregator.Multiplier(now))
	}
	if math.IsNaN(factor) || math.IsInf(factor, 0) || factor < 0 {
		return errors.New("invalid traffic multiplier")
	}
	var served map[int64]struct{}
	if len(logs) > 0 && aggregator.Served != nil {
		served, err = aggregator.Served(ctx, server.Id, protocol, protocolID)
		if err != nil {
			return err
		}
	}
	minute := now.UTC().Truncate(time.Minute)
	suffix := strconv.FormatInt(minute.Unix(), 10)
	increments := make(map[string]int64)
	totals := make(map[int64]int64)
	for _, report := range logs {
		if report.SID <= 0 || report.Upload < 0 || report.Download < 0 || report.Upload > MaxReportedTraffic || report.Download > MaxReportedTraffic-report.Upload {
			continue
		}
		if aggregator.Served != nil {
			if _, ok := served[report.SID]; !ok {
				continue
			}
		}
		total := report.Upload + report.Download
		if total == 0 || totals[report.SID] > MaxReportedTraffic-total {
			continue
		}
		totals[report.SID] += total
		if float64(total)*factor >= float64(math.MaxInt64) {
			return errors.New("scaled traffic exceeds int64")
		}
		upload, download := report.Upload, report.Download
		if factor != 1 {
			upload, download = int64(float64(upload)*factor), int64(float64(download)*factor)
		}
		field := fmt.Sprintf("%d|%d|", server.Id, report.SID)
		for direction, count := range map[string]int64{"u": upload, "d": download} {
			key := field + direction
			if increments[key] > math.MaxInt64-count {
				return errors.New("scaled report traffic overflow")
			}
			increments[key] += count
		}
	}
	bucket := bucketPrefix + suffix
	watchKeys := []string{bucket}
	marker := "traffic:report:" + reportID
	if reportID != "" {
		watchKeys = append(watchKeys, marker)
	}
	for attempt := 0; attempt < 8; attempt++ {
		err = aggregator.Redis.Watch(ctx, func(transaction *redis.Tx) error {
			if reportID != "" {
				exists, err := transaction.Exists(ctx, marker).Result()
				if err != nil || exists != 0 {
					return err
				}
			}
			currentValues, err := transaction.HGetAll(ctx, bucket).Result()
			if err != nil {
				return err
			}
			values := make(map[string]any, len(increments))
			for field, increment := range increments {
				current := int64(0)
				if text, ok := currentValues[field]; ok {
					current, err = strconv.ParseInt(text, 10, 64)
					if err != nil {
						return err
					}
				}
				if current < 0 || current > math.MaxInt64-increment {
					return errors.New("traffic bucket counter overflow")
				}
				values[field] = current + increment
			}
			_, err = transaction.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.HSet(ctx, serverReports, strconv.FormatInt(server.Id, 10), strconv.FormatInt(now.UnixMilli(), 10))
				if len(values) > 0 {
					pipe.HSet(ctx, bucket, values)
					pipe.Expire(ctx, bucket, bucketTTL)
					pipe.ZAdd(ctx, bucketIndex, redis.Z{Score: float64(minute.Unix()), Member: suffix})
				}
				if reportID != "" {
					pipe.Set(ctx, marker, "1", deadTTL)
				}
				return nil
			})
			return err
		}, watchKeys...)
		if !errors.Is(err, redis.TxFailedErr) {
			return err
		}
	}
	return err
}

var claimBucket = redis.NewScript(`
local suffix = ARGV[1]
if redis.call('EXISTS', KEYS[2]) == 1 then return suffix end
if redis.call('EXISTS', KEYS[1]) == 0 then redis.call('ZREM', KEYS[4], suffix); return '' end
if redis.call('EXISTS', KEYS[3]) == 1 then
  local origin = string.match(suffix, '^[^-]+')
  local counter = 'traffic:reissue:' .. origin
  local sequence = redis.call('INCR', counter)
  redis.call('EXPIRE', counter, 86400)
  local fresh = origin .. '-' .. sequence
  redis.call('RENAME', KEYS[1], 'traffic:processing:' .. fresh)
  redis.call('ZREM', KEYS[4], suffix)
  redis.call('ZADD', KEYS[4], ARGV[2], fresh)
  return fresh
end
redis.call('RENAME', KEYS[1], KEYS[2])
return suffix`)

var finishBucket = redis.NewScript(`
redis.call('SET', KEYS[2], '1', 'EX', 86400)
redis.call('DEL', KEYS[1], KEYS[5])
redis.call('ZREM', KEYS[3], ARGV[1])
if redis.call('EXISTS', KEYS[4]) == 1 then redis.call('ZADD', KEYS[3], ARGV[2], ARGV[1]) end
return 1`)

func (aggregator *Aggregator) Flush(ctx context.Context, now time.Time) error {
	lock, held, err := tasklock.Acquire(ctx, aggregator.Redis, "traffic:flush:lock", 5*time.Minute)
	if err != nil || !held {
		return err
	}
	defer lock.Release(ctx)
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stop := lock.KeepAlive(ctx, 5*time.Minute, cancel)
	defer stop()
	var failures []error
	for round := 0; round < 4; round++ {
		buckets, err := aggregator.Redis.ZRangeByScore(ctx, bucketIndex, &redis.ZRangeBy{Min: "-inf", Max: strconv.FormatInt(now.UTC().Truncate(time.Minute).Unix()-1, 10), Count: 1000}).Result()
		if err != nil {
			return err
		}
		if len(buckets) == 0 {
			break
		}
		for _, suffix := range buckets {
			if err := aggregator.flushBucket(ctx, suffix, now); err != nil {
				failures = append(failures, err)
			}
		}
		if len(failures) > 0 {
			break
		}
	}
	if err := aggregator.replayDeadLetters(ctx, now); err != nil {
		failures = append(failures, err)
	}
	if err := aggregator.flushServerReports(ctx); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func bucketTime(suffix string) (time.Time, error) {
	origin, _, _ := strings.Cut(suffix, "-")
	if len(origin) == 12 {
		return time.ParseInLocation("200601021504", origin, time.Local)
	}
	seconds, err := strconv.ParseInt(origin, 10, 64)
	if err != nil || seconds <= 0 || seconds%60 != 0 {
		return time.Time{}, fmt.Errorf("invalid traffic bucket %q", suffix)
	}
	return time.Unix(seconds, 0), nil
}

func (aggregator *Aggregator) flushBucket(ctx context.Context, suffix string, now time.Time) error {
	at, err := bucketTime(suffix)
	if err != nil {
		return aggregator.Redis.ZRem(ctx, bucketIndex, suffix).Err()
	}
	claimed, err := claimBucket.Run(ctx, aggregator.Redis, []string{bucketPrefix + suffix, processingPrefix + suffix, processedPrefix + suffix, bucketIndex}, suffix, at.Unix()).Text()
	if err != nil || claimed == "" {
		return err
	}
	values, err := aggregator.Redis.HGetAll(ctx, processingPrefix+claimed).Result()
	if err != nil {
		return err
	}
	deltas, err := parseDeltas(values)
	if err == nil {
		err = ApplyBatch(ctx, aggregator.Store, claimed, at, deltas)
	}
	if err != nil {
		count, countErr := aggregator.Redis.Incr(ctx, failurePrefix+claimed).Result()
		if countErr != nil {
			return countErr
		}
		if expiryErr := aggregator.Redis.Expire(ctx, failurePrefix+claimed, deadTTL).Err(); expiryErr != nil {
			return expiryErr
		}
		if count < 10 {
			return err
		}
		return aggregator.parkDeadLetter(ctx, claimed, now)
	}
	return finishBucket.Run(ctx, aggregator.Redis, []string{processingPrefix + claimed, processedPrefix + claimed, bucketIndex, bucketPrefix + claimed, failurePrefix + claimed}, claimed, at.Unix()).Err()
}

// parseDeltas folds the raw bucket fields into one delta per
// (server, subscription) pair.
//
// A field the fork does not understand is skipped rather than failing the
// whole bucket, matching upstream's parseTrafficDeltas. Failing the bucket
// instead would be destructive: flushBucket parks a repeatedly-failing bucket
// in the dead-letter index, every replay re-parses the same poisoned field, and
// once the dead-letter hash expires after deadTTL the "nothing left to parse"
// branch retires it as a success — silently discarding the valid entries that
// shared the bucket. A malformed field is a logging concern, not a reason to
// drop a minute of billable traffic.
func parseDeltas(values map[string]string) ([]Delta, error) {
	merged := make(map[string]Delta)
	for field, text := range values {
		parts := strings.Split(field, "|")
		if len(parts) != 3 {
			zap.S().Warnf("[trafficagg] skipping malformed traffic field %q", field)
			continue
		}
		serverID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || serverID <= 0 {
			zap.S().Warnf("[trafficagg] skipping traffic field %q with an invalid server id", field)
			continue
		}
		subscriptionID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || subscriptionID <= 0 {
			zap.S().Warnf("[trafficagg] skipping traffic field %q with an invalid subscription id", field)
			continue
		}
		count, err := strconv.ParseInt(text, 10, 64)
		if err != nil || count < 0 {
			zap.S().Warnf("[trafficagg] skipping traffic field %q with an invalid count %q", field, text)
			continue
		}
		switch parts[2] {
		case "u":
			delta := merged[parts[0]+"|"+parts[1]]
			delta.ServerID, delta.SubscriptionID = serverID, subscriptionID
			delta.Upload = count
			merged[parts[0]+"|"+parts[1]] = delta
		case "d":
			delta := merged[parts[0]+"|"+parts[1]]
			delta.ServerID, delta.SubscriptionID = serverID, subscriptionID
			delta.Download = count
			merged[parts[0]+"|"+parts[1]] = delta
		default:
			zap.S().Warnf("[trafficagg] skipping traffic field %q with an unknown direction %q", field, parts[2])
		}
	}
	deltas := make([]Delta, 0, len(merged))
	for _, delta := range merged {
		deltas = append(deltas, delta)
	}
	return deltas, nil
}

func (aggregator *Aggregator) parkDeadLetter(ctx context.Context, suffix string, now time.Time) error {
	return aggregator.Redis.Eval(ctx, `
if redis.call('EXISTS', KEYS[1]) == 1 then redis.call('RENAME', KEYS[1], KEYS[2]) end
redis.call('EXPIRE', KEYS[2], ARGV[3])
redis.call('ZADD', KEYS[3], ARGV[2], ARGV[1])
redis.call('SET', KEYS[4], '1', 'EX', 86400)
redis.call('ZREM', KEYS[5], ARGV[1])
redis.call('DEL', KEYS[6])
if redis.call('EXISTS', KEYS[7]) == 1 then redis.call('ZADD', KEYS[5], ARGV[4], ARGV[1]) end
return 1`, []string{processingPrefix + suffix, deadPrefix + suffix, deadIndex, processedPrefix + suffix, bucketIndex, failurePrefix + suffix, bucketPrefix + suffix}, suffix, now.Add(time.Minute).Unix(), int64(deadTTL/time.Second), mustBucketUnix(suffix)).Err()
}

func mustBucketUnix(suffix string) int64 { at, _ := bucketTime(suffix); return at.Unix() }

func (aggregator *Aggregator) replayDeadLetters(ctx context.Context, now time.Time) error {
	buckets, err := aggregator.Redis.ZRangeByScore(ctx, deadIndex, &redis.ZRangeBy{Min: "-inf", Max: strconv.FormatInt(now.Unix(), 10), Count: 5}).Result()
	if err != nil {
		return err
	}
	var failures []error
	for _, suffix := range buckets {
		values, err := aggregator.Redis.HGetAll(ctx, deadPrefix+suffix).Result()
		if err != nil {
			failures = append(failures, err)
			continue
		}
		deltas, parseErr := parseDeltas(values)
		at, timeErr := bucketTime(suffix)
		if parseErr != nil {
			err = parseErr
		} else if timeErr != nil {
			err = timeErr
		} else if len(values) > 0 {
			err = ApplyBatch(ctx, aggregator.Store, suffix, at, deltas)
		}
		if err == nil {
			pipe := aggregator.Redis.TxPipeline()
			pipe.Del(ctx, deadPrefix+suffix, failurePrefix+"replay:"+suffix)
			pipe.ZRem(ctx, deadIndex, suffix)
			_, err = pipe.Exec(ctx)
		} else {
			attempts, attemptErr := aggregator.Redis.Incr(ctx, failurePrefix+"replay:"+suffix).Result()
			if attemptErr != nil {
				failures = append(failures, attemptErr)
				continue
			}
			wait := time.Minute
			for attempt := int64(1); attempt < attempts && wait < 6*time.Hour; attempt++ {
				wait *= 2
			}
			pipe := aggregator.Redis.TxPipeline()
			pipe.Expire(ctx, failurePrefix+"replay:"+suffix, deadTTL)
			pipe.ZAdd(ctx, deadIndex, redis.Z{Score: float64(now.Add(min(wait, 6*time.Hour)).Unix()), Member: suffix})
			if _, retryErr := pipe.Exec(ctx); retryErr != nil {
				failures = append(failures, retryErr)
			}
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (aggregator *Aggregator) flushServerReports(ctx context.Context) error {
	values, err := aggregator.Redis.HGetAll(ctx, serverReports).Result()
	if err != nil {
		return err
	}
	reports := make(map[int64]time.Time, len(values))
	for field, value := range values {
		serverID, serverErr := strconv.ParseInt(field, 10, 64)
		millis, timeErr := strconv.ParseInt(value, 10, 64)
		if serverErr != nil || timeErr != nil || serverID <= 0 {
			continue
		}
		reports[serverID] = time.UnixMilli(millis).UTC()
	}
	if err := aggregator.Store.Node().UpdateServerLastReportedAtBatch(ctx, reports); err != nil {
		return err
	}
	acknowledge := redis.NewScript(`if redis.call('HGET', KEYS[1], ARGV[1]) == ARGV[2] then return redis.call('HDEL', KEYS[1], ARGV[1]) end return 0`)
	pipe := aggregator.Redis.Pipeline()
	for field, value := range values {
		acknowledge.Eval(ctx, pipe, []string{serverReports}, field, value)
	}
	_, err = pipe.Exec(ctx)
	return err
}
