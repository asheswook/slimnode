package fusefs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFetchPolicy_Defaults(t *testing.T) {
	p := NewFetchPolicy(FetchPolicyConfig{})
	assert.Equal(t, fetchModeAuto, p.Mode())
	assert.Equal(t, int64(defaultAutoGapToleranceKB*1024), p.gapToleranceBytes)
	assert.Equal(t, defaultAutoMinRangeRequests, p.minRangeRequests)
	assert.Equal(t, int64(defaultAutoMinSequentialMB*1024*1024), p.minSequentialBytes)
	assert.Equal(t, defaultAutoMinSequentialRate, p.minSequentialRate)
	assert.Equal(t, defaultAutoMaxBackwardSeeks, p.maxBackwardSeeks)
	assert.Equal(t, defaultAutoFileHintTTL, p.fileHintTTL)
	assert.Equal(t, defaultAutoPromotionCooldown, p.promotionCooldown)
}

func TestFetchPolicy_ResolveMode_AutoAndHint(t *testing.T) {
	now := time.Now()
	p := NewFetchPolicy(FetchPolicyConfig{Mode: fetchModeAuto, AutoFileHintTTL: time.Minute})

	assert.Equal(t, fetchModeRange, p.ResolveMode("blk00000.dat", now))
	p.MarkPromotionSuccess("blk00000.dat", now)
	assert.Equal(t, fetchModeFile, p.ResolveMode("blk00000.dat", now.Add(10*time.Second)))
	assert.Equal(t, fetchModeRange, p.ResolveMode("blk00000.dat", now.Add(2*time.Minute)))
}

func TestFetchPolicy_ResolveMode_Cooldown(t *testing.T) {
	now := time.Now()
	p := NewFetchPolicy(FetchPolicyConfig{Mode: fetchModeAuto, AutoPromotionCooldown: time.Minute})
	p.MarkPromotionFailure("blk00001.dat", now)

	assert.Equal(t, fetchModeRange, p.ResolveMode("blk00001.dat", now.Add(10*time.Second)))
	assert.Equal(t, fetchModeRange, p.ResolveMode("blk00001.dat", now.Add(2*time.Minute)))
}

func TestFetchPolicy_ObserveRangeReadAndPromotion(t *testing.T) {
	now := time.Now()
	p := NewFetchPolicy(FetchPolicyConfig{
		Mode:                  fetchModeAuto,
		AutoGapToleranceKB:    64,
		AutoMinRangeRequests:  3,
		AutoMinSequentialMB:   1,
		AutoMinSequentialRate: 0.8,
		AutoMaxBackwardSeeks:  1,
	})

	tracker := &SequentialReadTracker{}
	p.ObserveRangeRead(tracker, 0, 512*1024)
	p.ObserveRangeRead(tracker, 512*1024, 512*1024)
	p.ObserveRangeRead(tracker, 1024*1024, 512*1024)

	assert.True(t, p.ShouldPromoteToFile("blk00002.dat", tracker, now))
}

func TestFetchPolicy_NoPromotionForNonBlkFile(t *testing.T) {
	p := NewFetchPolicy(FetchPolicyConfig{Mode: fetchModeAuto})
	tracker := &SequentialReadTracker{rangeRequests: 1000, totalBytes: 10 * 1024 * 1024, sequentialBytes: 10 * 1024 * 1024}
	assert.False(t, p.ShouldPromoteToFile("rev00001.dat", tracker, time.Now()))
	assert.Equal(t, fetchModeRange, p.ResolveMode("rev00001.dat", time.Now()))
}
