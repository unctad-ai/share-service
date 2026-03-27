package main

import (
	"testing"
	"time"
)

func TestRateLimiterAllow(t *testing.T) {
	rl := NewRateLimiter(5, time.Minute)

	for i := 0; i < 5; i++ {
		if !rl.Allow("1.2.3.4") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	if rl.Allow("1.2.3.4") {
		t.Fatal("6th request should be denied")
	}
}

func TestRateLimiterDifferentIPs(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)

	rl.Allow("1.1.1.1")
	rl.Allow("1.1.1.1")

	if rl.Allow("1.1.1.1") {
		t.Fatal("1.1.1.1 should be rate limited")
	}
	if !rl.Allow("2.2.2.2") {
		t.Fatal("2.2.2.2 should be allowed")
	}
}

func TestRateLimiterExpiry(t *testing.T) {
	rl := NewRateLimiter(1, 50*time.Millisecond)

	rl.Allow("1.1.1.1")
	if rl.Allow("1.1.1.1") {
		t.Fatal("should be denied immediately")
	}

	time.Sleep(60 * time.Millisecond)

	if !rl.Allow("1.1.1.1") {
		t.Fatal("should be allowed after window expires")
	}
}
