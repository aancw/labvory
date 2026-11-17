package scheduler

import (
	"testing"
	"time"

	"github.com/aancw/lanvory/internal/store"
)

func TestDue(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	hoursAgo := func(h float64) *time.Time {
		t := now.Add(-time.Duration(h * float64(time.Hour)))
		return &t
	}
	seconds := func(s int) *int { return &s }

	cases := []struct {
		name  string
		integ store.Integration
		want  bool
	}{
		{"no interval → never", store.Integration{SyncIntervalSeconds: nil}, false},
		{"never synced → due", store.Integration{SyncIntervalSeconds: seconds(3600)}, true},
		{"interval elapsed", store.Integration{SyncIntervalSeconds: seconds(3600), LastSyncAt: hoursAgo(2)}, true},
		{"interval not elapsed", store.Integration{SyncIntervalSeconds: seconds(3600), LastSyncAt: hoursAgo(0.5)}, false},
		{"exactly at interval", store.Integration{SyncIntervalSeconds: seconds(3600), LastSyncAt: hoursAgo(1)}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := due(tc.integ, now); got != tc.want {
				t.Fatalf("due() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClassifyCert(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		notAfter time.Time
		want     certState
	}{
		{"expired yesterday", now.Add(-24 * time.Hour), certExpired},
		{"expired just now", now.Add(-time.Second), certExpired},
		{"inside warn window", now.Add(10 * 24 * time.Hour), certExpiring},
		{"last day of warn window", now.Add(certWarnDays*24*time.Hour - time.Second), certExpiring},
		{"outside warn window", now.Add(31 * 24 * time.Hour), certOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyCert(tc.notAfter, now); got != tc.want {
				t.Fatalf("classifyCert() = %v, want %v", got, tc.want)
			}
		})
	}
}
