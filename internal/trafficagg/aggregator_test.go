package trafficagg

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/redis/go-redis/v9"
)

type unavailableTrafficStore struct {
	repository.Store
	attempts int
}

func (store *unavailableTrafficStore) InTx(context.Context, func(repository.Store) error) error {
	store.attempts++
	return errors.New("temporary database failure")
}

func TestDeadLetterBackoffAndRecovery(t *testing.T) {
	store := accountingStore(t)
	ctx := context.Background()
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store.Ent().Server.Create().SetID(1).SaveX(ctx)
	store.Ent().UserSubscribe.Create().SetID(1).SetUserID(7).SetOrderID(1).SetSubscribeID(3).SaveX(ctx)
	unavailable := &unavailableTrafficStore{Store: store}
	aggregator := &Aggregator{Store: unavailable, Redis: client}
	now := time.Now().UTC().Truncate(time.Minute)
	server := &node.Server{Id: 1, Protocols: `[{"type":"vless","id":"1"}]`}
	if err := aggregator.AddReportAt(ctx, server, "vless", "1", []Report{{SID: 1, Upload: 16777217, Download: 1}}, now); err != nil {
		t.Fatal(err)
	}
	suffix := strconv.FormatInt(now.Unix(), 10)
	for attempt := 0; attempt < 10; attempt++ {
		err := aggregator.flushBucket(ctx, suffix, now)
		if attempt < 9 && err == nil {
			t.Fatal("database failure was hidden")
		}
		if attempt == 9 && err != nil {
			t.Fatal(err)
		}
	}
	if client.Exists(ctx, deadPrefix+suffix).Val() != 1 || client.ZCard(ctx, deadIndex).Val() != 1 || store.Ent().TrafficBatch.Query().CountX(ctx) != 0 {
		t.Fatal("failed traffic was not retained as an uncommitted dead letter")
	}
	if err := aggregator.replayDeadLetters(ctx, now); err != nil {
		t.Fatal(err)
	}
	if unavailable.attempts != 10 {
		t.Fatal("dead letter replayed before its due time")
	}
	if err := aggregator.replayDeadLetters(ctx, now.Add(time.Minute)); err == nil {
		t.Fatal("failed replay was hidden")
	}
	if err := aggregator.replayDeadLetters(ctx, now.Add(90*time.Second)); err != nil {
		t.Fatal(err)
	}
	if unavailable.attempts != 11 {
		t.Fatal("dead letter ignored retry backoff")
	}
	aggregator.Store = store
	if err := aggregator.replayDeadLetters(ctx, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := aggregator.replayDeadLetters(ctx, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	subscription := store.Ent().UserSubscribe.GetX(ctx, 1)
	if subscription.Upload != 16777217 || subscription.Download != 1 || client.ZCard(ctx, deadIndex).Val() != 0 || client.Exists(ctx, deadPrefix+suffix).Val() != 0 {
		t.Fatalf("dead letter recovery lost precision, replayed or left stale data: %+v", subscription)
	}
}

func TestMeteredReportRetryAcrossMinutesKeepsOriginalCharge(t *testing.T) {
	store := accountingStore(t)
	ctx := t.Context()
	db := store.Ent()
	cache := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	server := db.Server.Create().SetName("Metered relay").SaveX(ctx)
	sub := db.UserSubscribe.Create().SetUserID(8).SetOrderID(1).SetSubscribeID(3).SaveX(ctx)
	request := []Report{{SID: sub.ID, Upload: 12, Download: 16}}
	start := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	aggregator := &Aggregator{Store: store, Redis: cache, Multiplier: func(at time.Time) float32 {
		if at.Before(start.Add(time.Minute)) {
			return 1.5
		}
		return 0.5
	}}
	relay := &node.Server{Id: server.ID, Protocols: `[{"type":"vless","id":"billed","ratio":2}]`}
	for _, input := range []struct {
		id string
		at time.Time
	}{
		{"first-report", start}, {"first-report", start.Add(time.Minute)}, {"second-report", start.Add(time.Minute)},
	} {
		if err := aggregator.AddReportOnceAt(ctx, relay, "vless", "billed", request, input.at, input.id); err != nil {
			t.Fatal(err)
		}
	}
	unavailable := &unavailableTrafficStore{Store: store}
	aggregator.Store = unavailable
	if err := aggregator.Flush(ctx, start.Add(2*time.Minute)); err == nil {
		t.Fatal("flush succeeded while accounting storage was unavailable")
	}
	if stored := db.UserSubscribe.GetX(ctx, sub.ID); stored.Upload != 0 || stored.Download != 0 || db.TrafficLog.Query().CountX(ctx) != 0 {
		t.Fatal("interrupted flush posted a partial charge")
	}
	aggregator.Store = store
	for range 2 {
		if err := aggregator.Flush(ctx, start.Add(3*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	stored := db.UserSubscribe.GetX(ctx, sub.ID)
	if stored.Upload != 48 || stored.Download != 64 || db.TrafficBatch.Query().CountX(ctx) != 2 {
		t.Fatalf("recovered metering: upload %d, download %d, batches %d", stored.Upload, stored.Download, db.TrafficBatch.Query().CountX(ctx))
	}
	rows := db.TrafficLog.Query().AllX(ctx)
	if len(rows) != 2 {
		t.Fatalf("metering produced %d ledger rows, want 2", len(rows))
	}
	charges := map[int64][2]int64{start.Unix(): {36, 48}, start.Add(time.Minute).Unix(): {12, 16}}
	for _, row := range rows {
		if want, found := charges[row.Timestamp.Unix()]; !found || want != [2]int64{row.Upload, row.Download} || row.SubscribeID != sub.ID || row.ServerID != relay.Id || row.UserID != sub.UserID {
			t.Fatalf("unexpected ledger charge: %+v", row)
		}
		delete(charges, row.Timestamp.Unix())
	}
}

func TestMinuteBucketsReissueLateReportsAndReplayCommittedBatch(t *testing.T) {
	store := accountingStore(t)
	ctx := context.Background()
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store.Ent().Server.Create().SetID(4).SaveX(ctx)
	store.Ent().UserSubscribe.Create().SetID(1).SetUserID(7).SetOrderID(1).SetSubscribeID(3).SaveX(ctx)
	aggregator := &Aggregator{Store: store, Redis: client, Served: func(context.Context, int64, string, string) (map[int64]struct{}, error) {
		return map[int64]struct{}{1: {}}, nil
	}}
	now := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)
	server := &node.Server{Id: 4, Protocols: `[{"type":"vless","id":"1","ratio":1},{"type":"vless","id":"2","ratio":2}]`}
	if err := aggregator.AddReportAt(ctx, server, "vless", "2", []Report{{SID: 1, Upload: 7, Download: 9}, {SID: 2, Upload: 100}, {SID: 1, Upload: -1}}, now); err != nil {
		t.Fatal(err)
	}
	if err := aggregator.Flush(ctx, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if subscription := store.Ent().UserSubscribe.GetX(ctx, 1); subscription.Upload != 14 || subscription.Download != 18 {
		t.Fatalf("instance ratio/scope = %+v", subscription)
	}
	suffix := strconv.FormatInt(now.Unix(), 10)
	if err := client.HSet(ctx, processingPrefix+suffix, "4|1|u", "14", "4|1|d", "18").Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.ZAdd(ctx, bucketIndex, redis.Z{Score: float64(now.Unix()), Member: suffix}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := aggregator.Flush(ctx, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if store.Ent().UserSubscribe.GetX(ctx, 1).Upload != 14 {
		t.Fatal("committed bucket replay charged twice")
	}
	if err := aggregator.AddReportAt(ctx, server, "vless", "1", []Report{{SID: 1, Upload: 3}}, now.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := aggregator.Flush(ctx, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if store.Ent().UserSubscribe.GetX(ctx, 1).Upload != 17 || store.Ent().TrafficBatch.Query().CountX(ctx) != 2 {
		t.Fatal("late traffic was lost or reused a committed batch")
	}
}

func TestBucketTimeDistinguishesRepeatedWallClockMinutes(t *testing.T) {
	first := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)
	second := first.Add(time.Hour)
	for _, instant := range []time.Time{first, second} {
		parsed, err := bucketTime(strconv.FormatInt(instant.Unix(), 10) + "-1")
		if err != nil || !parsed.Equal(instant) {
			t.Fatalf("minute %s = %s, %v", instant, parsed, err)
		}
	}
}

func TestReportReplayAndOverflowAreAtomic(t *testing.T) {
	ctx := context.Background()
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	aggregator := &Aggregator{Redis: client}
	server := &node.Server{Id: 1, Protocols: `[{"type":"vless","id":"1"}]`}
	now := time.Now().UTC().Truncate(time.Minute)
	for attempt := 0; attempt < 2; attempt++ {
		if err := aggregator.AddReportOnceAt(ctx, server, "vless", "1", []Report{{SID: 1, Upload: 7, Download: 9}}, now, "legacy-task-1"); err != nil {
			t.Fatal(err)
		}
	}
	key := bucketPrefix + strconv.FormatInt(now.Unix(), 10)
	if value := client.HGet(ctx, key, "1|1|u").Val(); value != "7" {
		t.Fatalf("report billed twice: %s", value)
	}
	if err := client.HSet(ctx, key, "1|1|d", "9223372036854775807").Err(); err != nil {
		t.Fatal(err)
	}
	if err := aggregator.AddReportAt(ctx, server, "vless", "1", []Report{{SID: 1, Upload: 7, Download: 9}}, now); err == nil {
		t.Fatal("overflow accepted")
	}
	if value := client.HGet(ctx, key, "1|1|u").Val(); value != "7" {
		t.Fatal("failed report partially changed bucket")
	}
}

func TestHeartbeatBatchSkipsDeletedServersAndKeepsNewerReports(t *testing.T) {
	store := accountingStore(t)
	ctx := context.Background()
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	defer client.Close()
	now := time.Now().UTC().Truncate(time.Millisecond)
	store.Ent().Server.Create().SetID(1).SetName("preserved").SaveX(ctx)
	store.Ent().Server.Create().SetID(2).SetLastReportedAt(now.Add(time.Minute)).SaveX(ctx)
	if err := client.HSet(ctx, serverReports, "1", now.UnixMilli(), "2", now.UnixMilli(), "999", now.UnixMilli(), "invalid", "data").Err(); err != nil {
		t.Fatal(err)
	}
	if err := (&Aggregator{Store: store, Redis: client}).flushServerReports(ctx); err != nil {
		t.Fatal(err)
	}
	first, second := store.Ent().Server.GetX(ctx, 1), store.Ent().Server.GetX(ctx, 2)
	if first.Name != "preserved" || first.LastReportedAt == nil || !first.LastReportedAt.Equal(now) || second.LastReportedAt == nil || !second.LastReportedAt.Equal(now.Add(time.Minute)) || client.HLen(ctx, serverReports).Val() != 0 {
		t.Fatalf("heartbeat batch lost updates or left a poison entry: %+v %+v", first, second)
	}
}

// A field the parser does not recognise must be skipped, not fail the bucket.
//
// Failing the bucket is destructive rather than merely lossy: flushBucket parks
// a repeatedly-failing bucket in the dead-letter index, every replay re-parses
// the same poisoned field, and once the dead-letter hash expires the
// "nothing left to parse" branch retires it as a success. Every valid entry
// that shared the bucket is then discarded without ever being billed.
func TestMalformedFieldDoesNotDiscardTheRestOfTheBucket(t *testing.T) {
	deltas, err := parseDeltas(map[string]string{
		"4|1|d": "20",
		// Unrecognised shapes and values, each of which used to abort the whole
		// parse: a 4-part name, a non-numeric count, an unknown direction, a
		// non-numeric server id and a non-positive subscription id.
		"4|1|u|x":  "5",
		"4|3|u":    "not-a-number",
		"4|1|q":    "7",
		"nope|1|u": "9",
		"4|0|u":    "11",
		"4|-1|d":   "13",
		"4|2|u":    "17",
		"4|1|u":    "10",
	})
	if err != nil {
		t.Fatalf("a malformed field failed the whole bucket: %v", err)
	}

	bySubscription := make(map[int64]Delta, len(deltas))
	for _, delta := range deltas {
		bySubscription[delta.SubscriptionID] = delta
	}

	// The well-formed entries survive: subscription 1 (both directions) and
	// subscription 2 (upload only). Subscription 3 had an unusable count, so it
	// contributes nothing rather than poisoning the bucket.
	if len(bySubscription) != 2 {
		t.Fatalf("deltas = %+v, want exactly two subscriptions", deltas)
	}
	second, ok := bySubscription[2]
	if !ok || second.ServerID != 4 || second.Upload != 17 || second.Download != 0 {
		t.Fatalf("subscription 2 = %+v, want server 4 upload 17", second)
	}
	first, ok := bySubscription[1]
	if !ok || first.ServerID != 4 {
		t.Fatalf("subscription 1 = %+v, want server 4", first)
	}
	if first.Upload != 10 || first.Download != 20 {
		t.Fatalf("subscription 1 = %+v, want upload 10 download 20", first)
	}
}

// A bucket whose fields are all unusable parses to nothing without erroring, so
// the flush commits the batch marker and the bucket is retired normally instead
// of being parked as a poison dead letter.
func TestFullyMalformedBucketParsesToNothing(t *testing.T) {
	deltas, err := parseDeltas(map[string]string{
		"garbage":     "1",
		"4|1|u":       "nope",
		"4|1|unknown": "3",
	})
	if err != nil {
		t.Fatalf("fully malformed bucket errored: %v", err)
	}
	if len(deltas) != 0 {
		t.Fatalf("deltas = %+v, want none", deltas)
	}
}
