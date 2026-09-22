package mnp

import (
	"context"
	"testing"
	"time"

	"github.com/negz/mnp/internal/db"
)

// fakeClientStore satisfies ClientStore. It embeds a MockStore for the ETL
// methods (unused here) and serves metadata from an in-memory map.
type fakeClientStore struct {
	*MockStore
	metadata map[string]string
}

// Rebuild is unused by these tests; it exists only to satisfy ClientStore.
func (f *fakeClientStore) Rebuild(_ context.Context, _ func(tx *db.SQLiteStore) error) error {
	return nil
}

func (f *fakeClientStore) GetMetadata(_ context.Context, key string) (string, error) {
	return f.metadata[key], nil
}

func (f *fakeClientStore) SetMetadata(_ context.Context, key, value string) error {
	f.metadata[key] = value
	return nil
}

func TestNeedsFullSync(t *testing.T) {
	now := time.Now()

	cases := map[string]struct {
		reason   string
		interval time.Duration
		force    bool
		metadata map[string]string
		want     bool
	}{
		"Forced": {
			reason:   "A forced sync is always full.",
			interval: 24 * time.Hour,
			force:    true,
			want:     true,
		},
		"NonPositiveInterval": {
			reason:   "A non-positive interval makes every sync full.",
			interval: 0,
			want:     true,
		},
		"NeverSynced": {
			reason:   "With no recorded full sync, the next sync is full.",
			interval: 24 * time.Hour,
			metadata: map[string]string{},
			want:     true,
		},
		"Recent": {
			reason:   "A full sync within the interval means the next sync is incremental.",
			interval: 24 * time.Hour,
			metadata: map[string]string{lastFullSyncKey: now.Add(-time.Hour).Format(time.RFC3339)},
			want:     false,
		},
		"Stale": {
			reason:   "A full sync older than the interval means the next sync is full.",
			interval: 24 * time.Hour,
			metadata: map[string]string{lastFullSyncKey: now.Add(-48 * time.Hour).Format(time.RFC3339)},
			want:     true,
		},
		"CorruptTimestamp": {
			reason:   "An unparseable timestamp forces a full sync to recover.",
			interval: 24 * time.Hour,
			metadata: map[string]string{lastFullSyncKey: "not-a-time"},
			want:     true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := &Client{
				store:            &fakeClientStore{metadata: tc.metadata},
				fullSyncInterval: tc.interval,
			}
			got, err := c.needsFullSync(context.Background(), tc.force)
			if err != nil {
				t.Fatalf("needsFullSync: %v", err)
			}
			if got != tc.want {
				t.Errorf("\n%s\nneedsFullSync(force=%v) = %v, want %v", tc.reason, tc.force, got, tc.want)
			}
		})
	}
}
