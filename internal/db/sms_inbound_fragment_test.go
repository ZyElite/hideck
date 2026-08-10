package db

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestSMSInboundFragmentPersistsAcrossDatabaseReopen(t *testing.T) {
	previousDB := DB
	path := filepath.Join(t.TempDir(), "fragments.db")
	if err := Init(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if DB != nil {
			if sqlDB, err := DB.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
		DB = previousDB
	})
	scope := SMSInboundFragmentScope{
		DeviceID: "wwan0", IMSI: "234102356143376", SessionKey: "sender=giffgaff|ref=198",
	}
	fragment := SMSInboundFragment{
		Reference: 198, ReferenceBits: 8, Total: 2, Sequence: 1,
		Content: "first ", ArrivedAt: time.Now(), RPMR: 61, CallID: "part-1",
	}
	result, err := SaveSMSInboundFragment(scope, fragment)
	if err != nil || !result.Inserted || len(result.Fragments) != 1 {
		t.Fatalf("save result=%#v err=%v", result, err)
	}
	closeSMSFragmentTestDB(t)
	if err := Init(path); err != nil {
		t.Fatal(err)
	}
	rows, err := LoadSMSInboundFragments(scope)
	if err != nil || len(rows) != 1 || rows[0].Content != "first " {
		t.Fatalf("reopened rows=%#v err=%v", rows, err)
	}
	assertSMSInboundFragmentDuplicateAndCollision(t, scope, fragment)
	ackedAt := time.Now().Add(time.Second)
	if err := MarkSMSInboundFragmentAcked(scope, 1, ackedAt); err != nil {
		t.Fatal(err)
	}
	rows, err = LoadSMSInboundFragments(scope)
	if err != nil || len(rows) != 1 || !rows[0].AckSent || rows[0].AckSentAt.Before(ackedAt) {
		t.Fatalf("acked rows=%#v err=%v", rows, err)
	}
	if err := DeleteSMSInboundFragments(scope); err != nil {
		t.Fatal(err)
	}
	rows, err = LoadSMSInboundFragments(scope)
	if err != nil || len(rows) != 0 {
		t.Fatalf("rows after delete=%#v err=%v", rows, err)
	}
}

func assertSMSInboundFragmentDuplicateAndCollision(
	t *testing.T,
	scope SMSInboundFragmentScope,
	fragment SMSInboundFragment,
) {
	t.Helper()
	duplicate, err := SaveSMSInboundFragment(scope, fragment)
	if err != nil || duplicate.Inserted || len(duplicate.Fragments) != 1 {
		t.Fatalf("duplicate result=%#v err=%v", duplicate, err)
	}
	fragment.Content = "changed"
	collision, err := SaveSMSInboundFragment(scope, fragment)
	if !errors.Is(err, ErrSMSInboundFragmentCollision) || collision.CollisionReason != "sequence_content_mismatch" {
		t.Fatalf("collision result=%#v err=%v", collision, err)
	}
}

func closeSMSFragmentTestDB(t *testing.T) {
	t.Helper()
	sqlDB, err := DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	DB = nil
}
