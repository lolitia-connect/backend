package traffic

import (
	"database/sql"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestCorrectedTrafficReconcilesAcrossReportingMonths(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	model := NewModel(client)
	february := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	january := february.Add(-time.Second)
	report := &TrafficLog{ServerId: 7, UserId: 15, SubscribeId: 23, Download: 80, Upload: 20, Timestamp: january}
	other := &TrafficLog{ServerId: 8, UserId: 16, SubscribeId: 24, Download: 50, Upload: 10, Timestamp: february}
	require.NoError(t, model.Insert(ctx, report))
	require.NoError(t, model.Insert(ctx, other))
	checkMonth := func(day time.Time, want TotalTraffic) {
		t.Helper()
		total, err := model.QueryTrafficByMonthly(ctx, day)
		require.NoError(t, err)
		require.Equal(t, want, *total)
	}
	checkMonth(january, TotalTraffic{Download: 80, Upload: 20})
	checkMonth(february, TotalTraffic{Download: 50, Upload: 10})
	stored, err := model.FindOne(ctx, report.Id)
	require.NoError(t, err)
	stored.Timestamp, stored.Download = february, 120
	require.NoError(t, model.Update(ctx, stored))
	checkMonth(january, TotalTraffic{})
	checkMonth(february, TotalTraffic{Download: 170, Upload: 30})
	servers, err := model.TopServersTrafficByDay(ctx, february, 1)
	require.NoError(t, err)
	require.Equal(t, []ServerTrafficRanking{{ServerId: 7, Download: 120, Upload: 20, Total: 140}}, servers)
	users, err := model.TopUsersTrafficByMonthly(ctx, february, 1)
	require.NoError(t, err)
	require.Equal(t, []UserTrafficRanking{{UserId: 15, SubscribeId: 23, Download: 120, Upload: 20, Total: 140}}, users)
	details, count, err := model.QueryTrafficLogPageList(ctx, 15, 23, 1, 5)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.Len(t, details, 1)
	require.Equal(t, stored.Id, details[0].Id)
	require.True(t, february.Equal(details[0].Timestamp))
	require.EqualValues(t, 140, details[0].Download+details[0].Upload)
	require.NoError(t, model.Delete(ctx, report.Id))
	require.NoError(t, model.Delete(ctx, report.Id))
	checkMonth(february, TotalTraffic{Download: 50, Upload: 10})
}

func TestDailyRankingsUseLocalMidnightAcrossClockChanges(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, date := range []time.Time{
		time.Date(2025, time.March, 9, 12, 0, 0, 0, loc),
		time.Date(2025, time.November, 2, 12, 0, 0, 0, loc),
	} {
		start, end := dayRange(date)
		midnight := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
		if !start.Equal(midnight) || !end.Equal(midnight.AddDate(0, 0, 1)) {
			t.Errorf("daily ranking for %s spans %s to %s", date.Format(time.DateOnly), start, end)
		}
	}
}

func TestTrafficRanges(t *testing.T) {
	date := time.Date(2026, 5, 22, 13, 14, 15, 0, time.UTC)
	start, end := dayRange(date)
	if !start.Equal(time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected day start: %v", start)
	}
	if !end.Equal(time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected day end: %v", end)
	}

	start, end = monthRange(date)
	if !start.Equal(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected month start: %v", start)
	}
	if !end.Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected month end: %v", end)
	}
}
