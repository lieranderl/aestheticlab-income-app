package web

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTheLoginSourceIsTheAddressCloudRunAppended(t *testing.T) {
	for _, c := range []struct{ remote, xff, want string }{
		{"192.0.2.1:5555", "", "192.0.2.1"},
		{"169.254.1.1:5555", "198.51.100.7", "198.51.100.7"},
		{"169.254.1.1:5555", "203.0.113.1, 198.51.100.7", "198.51.100.7"},
		{"169.254.1.1:5555", "2001:db8:1:2:aaaa::1", "2001:db8:1:2::/64"},
		{"169.254.1.1:5555", "2001:db8:1:2:bbbb::9", "2001:db8:1:2::/64"},
	} {
		r := httptest.NewRequest("POST", "/login", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := loginSource(r); got != c.want {
			t.Errorf("remote %s, X-Forwarded-For %q: source %q, want %q", c.remote, c.xff, got, c.want)
		}
	}
}

func TestAFloodOfSourcesSharesOneCountOnceTheTableIsFull(t *testing.T) {
	clock := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	th := newLoginThrottle(func() time.Time { return clock })
	for i := range maxSources {
		th.admit(fmt.Sprintf("src-%d", i))
	}
	for i := range sourceLimit {
		th.admit(fmt.Sprintf("new-%d", i))
	}
	if th.admit("another-new-source") {
		t.Error("a new source is not limited while the table is full")
	}
	if !th.admit("src-1") {
		t.Error("a tracked source with one attempt is refused")
	}
	clock = clock.Add(sourceWindow)
	th.admit("after-the-window")
	if len(th.sources) != 1 || !th.admit("another-new-source") {
		t.Errorf("old counts not pruned: %d sources", len(th.sources))
	}
}

func TestPasswordChecksWaitForAFreeSlot(t *testing.T) {
	th := newLoginThrottle(time.Now)
	for range maxHashing {
		th.hashing <- struct{}{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ran := false
	if th.hash(httptest.NewRequest("POST", "/login", nil).WithContext(ctx), func() { ran = true }) || ran {
		t.Fatal("a password check ran with every slot taken")
	}
	<-th.hashing
	if !th.hash(httptest.NewRequest("POST", "/login", nil), func() { ran = true }) || !ran {
		t.Fatal("a password check did not run in a free slot")
	}
}

func TestAttemptsAreCountedWhenAdmittedNotWhenTheyFail(t *testing.T) {
	th := newLoginThrottle(time.Now)
	// Ten logins in flight at once, none finished yet.
	for i := range sourceLimit {
		if !th.admit("198.51.100.7") {
			t.Fatalf("attempt %d refused", i+1)
		}
	}
	if th.admit("198.51.100.7") {
		t.Fatal("an 11th parallel attempt was admitted")
	}
	th.release("198.51.100.7") // one was never checked
	if !th.admit("198.51.100.7") {
		t.Fatal("a released attempt was not given back")
	}
	th.succeeded("198.51.100.7")
	if !th.admit("198.51.100.7") {
		t.Fatal("a successful login did not clear the count")
	}
}
