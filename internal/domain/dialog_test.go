package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBuildPersonalKey_IsOrderIndependent(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	forward, err := BuildPersonalKey(first, second)
	if err != nil {
		t.Fatal(err)
	}
	reverse, err := BuildPersonalKey(second, first)
	if err != nil {
		t.Fatal(err)
	}
	if forward != reverse {
		t.Fatalf("personal key depends on participant order")
	}
	if _, err := BuildPersonalKey(first, first); err == nil {
		t.Fatal("self dialog must be rejected")
	}
}

func TestMember_AdvanceReadChangesOnlyReceiver(t *testing.T) {
	now := time.Now().UTC()
	dialogID := uuid.New()
	first := Member{
		DialogID: dialogID, UserID: uuid.New(), Role: MemberRoleMember, Status: MemberStatusActive,
		UnreadCount: 20, LastReadMessageSequence: 100, AddedBy: uuid.New(), JoinedAt: now, UpdatedAt: now,
	}
	second := Member{
		DialogID: dialogID, UserID: uuid.New(), Role: MemberRoleMember, Status: MemberStatusActive,
		UnreadCount: 20, LastReadMessageSequence: 100, AddedBy: uuid.New(), JoinedAt: now, UpdatedAt: now,
	}
	changed, err := first.AdvanceRead(105, 120, 5, now.Add(time.Second))
	if err != nil || !changed {
		t.Fatalf("advance read: changed=%v err=%v", changed, err)
	}
	if first.LastReadMessageSequence != 105 || first.UnreadCount != 15 {
		t.Fatalf("unexpected first member state: %+v", first)
	}
	if second.LastReadMessageSequence != 100 || second.UnreadCount != 20 {
		t.Fatalf("another member state changed: %+v", second)
	}
}

func TestMember_AdvanceReadIsMonotonic(t *testing.T) {
	now := time.Now().UTC()
	member := Member{
		DialogID: uuid.New(), UserID: uuid.New(), Role: MemberRoleMember, Status: MemberStatusActive,
		UnreadCount: 5, LastReadMessageSequence: 115, AddedBy: uuid.New(), JoinedAt: now, UpdatedAt: now,
	}
	changed, err := member.AdvanceRead(110, 120, 5, now)
	if err != nil || changed {
		t.Fatalf("older read must be an idempotent no-op: changed=%v err=%v", changed, err)
	}
	if member.LastReadMessageSequence != 115 || member.UnreadCount != 5 {
		t.Fatalf("read state regressed: %+v", member)
	}
	if _, err := member.AdvanceRead(121, 120, 1, now); err == nil {
		t.Fatal("cursor beyond dialog maximum must fail")
	}
}

func TestMember_MarkAllRead(t *testing.T) {
	now := time.Now().UTC()
	member := Member{
		DialogID: uuid.New(), UserID: uuid.New(), Role: MemberRoleMember, Status: MemberStatusActive,
		UnreadCount: 20, LastReadMessageSequence: 100, AddedBy: uuid.New(), JoinedAt: now, UpdatedAt: now,
	}
	changed, err := member.MarkAllRead(120, now)
	if err != nil || !changed || member.LastReadMessageSequence != 120 || member.UnreadCount != 0 {
		t.Fatalf("mark all failed: changed=%v member=%+v err=%v", changed, member, err)
	}
}
