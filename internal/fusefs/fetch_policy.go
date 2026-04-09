package fusefs

import (
	"sync"
	"time"
)

const (
	fetchModeAuto  = "auto"
	fetchModeFile  = "file"
	fetchModeRange = "range"
)

const (
	defaultAutoGapToleranceKB    = 64
	defaultAutoMinRangeRequests  = 256
	defaultAutoMinSequentialMB   = 4
	defaultAutoMinSequentialRate = 0.90
	defaultAutoMaxBackwardSeeks  = 2
	defaultAutoFileHintTTL       = 10 * time.Minute
	defaultAutoPromotionCooldown = 30 * time.Second
)

type FetchPolicyConfig struct {
	Mode                  string
	AutoGapToleranceKB    int
	AutoMinRangeRequests  int
	AutoMinSequentialMB   int
	AutoMinSequentialRate float64
	AutoMaxBackwardSeeks  int
	AutoFileHintTTL       time.Duration
	AutoPromotionCooldown time.Duration
}

type FetchPolicy struct {
	mode               string
	gapToleranceBytes  int64
	minRangeRequests   int
	minSequentialBytes int64
	minSequentialRate  float64
	maxBackwardSeeks   int
	fileHintTTL        time.Duration
	promotionCooldown  time.Duration

	mu            sync.RWMutex
	fileHints     map[string]time.Time
	cooldownUntil map[string]time.Time
}

type SequentialReadTracker struct {
	initialized     bool
	lastEndOffset   int64
	rangeRequests   int
	totalBytes      int64
	sequentialBytes int64
	backwardSeeks   int
}

func NewFetchPolicy(cfg FetchPolicyConfig) *FetchPolicy {
	zeroConfig := cfg.AutoGapToleranceKB == 0 &&
		cfg.AutoMinRangeRequests == 0 &&
		cfg.AutoMinSequentialMB == 0 &&
		cfg.AutoMinSequentialRate == 0 &&
		cfg.AutoMaxBackwardSeeks == 0 &&
		cfg.AutoFileHintTTL == 0 &&
		cfg.AutoPromotionCooldown == 0

	mode := cfg.Mode
	if mode == "" {
		mode = fetchModeAuto
	}

	gapToleranceKB := cfg.AutoGapToleranceKB
	if gapToleranceKB < 0 || zeroConfig {
		gapToleranceKB = defaultAutoGapToleranceKB
	}

	minRangeRequests := cfg.AutoMinRangeRequests
	if minRangeRequests < 1 || zeroConfig {
		minRangeRequests = defaultAutoMinRangeRequests
	}

	minSequentialMB := cfg.AutoMinSequentialMB
	if minSequentialMB < 1 || zeroConfig {
		minSequentialMB = defaultAutoMinSequentialMB
	}

	minSequentialRate := cfg.AutoMinSequentialRate
	if minSequentialRate <= 0 || minSequentialRate > 1 || zeroConfig {
		minSequentialRate = defaultAutoMinSequentialRate
	}

	maxBackwardSeeks := cfg.AutoMaxBackwardSeeks
	if maxBackwardSeeks < 0 || zeroConfig {
		maxBackwardSeeks = defaultAutoMaxBackwardSeeks
	}

	fileHintTTL := cfg.AutoFileHintTTL
	if fileHintTTL <= 0 || zeroConfig {
		fileHintTTL = defaultAutoFileHintTTL
	}

	promotionCooldown := cfg.AutoPromotionCooldown
	if promotionCooldown <= 0 || zeroConfig {
		promotionCooldown = defaultAutoPromotionCooldown
	}

	return &FetchPolicy{
		mode:               mode,
		gapToleranceBytes:  int64(gapToleranceKB) * 1024,
		minRangeRequests:   minRangeRequests,
		minSequentialBytes: int64(minSequentialMB) * 1024 * 1024,
		minSequentialRate:  minSequentialRate,
		maxBackwardSeeks:   maxBackwardSeeks,
		fileHintTTL:        fileHintTTL,
		promotionCooldown:  promotionCooldown,
		fileHints:          make(map[string]time.Time),
		cooldownUntil:      make(map[string]time.Time),
	}
}

func (p *FetchPolicy) Mode() string {
	return p.mode
}

func (p *FetchPolicy) ResolveMode(filename string, now time.Time) string {
	if p.mode != fetchModeAuto {
		return p.mode
	}

	if !isAdaptiveTargetFile(filename) {
		return fetchModeRange
	}

	p.mu.RLock()
	cooldownUntil, inCooldown := p.cooldownUntil[filename]
	hintUntil, hasHint := p.fileHints[filename]
	p.mu.RUnlock()

	if inCooldown && now.Before(cooldownUntil) {
		return fetchModeRange
	}

	if hasHint && now.Before(hintUntil) {
		return fetchModeFile
	}

	if hasHint && !now.Before(hintUntil) {
		p.mu.Lock()
		if currentHint, ok := p.fileHints[filename]; ok && !now.Before(currentHint) {
			delete(p.fileHints, filename)
		}
		p.mu.Unlock()
	}

	return fetchModeRange
}

func (p *FetchPolicy) MarkPromotionSuccess(filename string, now time.Time) {
	p.mu.Lock()
	p.fileHints[filename] = now.Add(p.fileHintTTL)
	delete(p.cooldownUntil, filename)
	p.mu.Unlock()
}

func (p *FetchPolicy) MarkPromotionFailure(filename string, now time.Time) {
	p.mu.Lock()
	p.cooldownUntil[filename] = now.Add(p.promotionCooldown)
	delete(p.fileHints, filename)
	p.mu.Unlock()
}

func (p *FetchPolicy) ShouldPromoteToFile(filename string, tracker *SequentialReadTracker, now time.Time) bool {
	if p.mode != fetchModeAuto {
		return false
	}
	if !isAdaptiveTargetFile(filename) {
		return false
	}
	if tracker == nil {
		return false
	}
	if tracker.rangeRequests < p.minRangeRequests {
		return false
	}
	if tracker.sequentialBytes < p.minSequentialBytes {
		return false
	}
	if tracker.totalBytes <= 0 {
		return false
	}
	if tracker.backwardSeeks > p.maxBackwardSeeks {
		return false
	}
	if float64(tracker.sequentialBytes)/float64(tracker.totalBytes) < p.minSequentialRate {
		return false
	}

	p.mu.RLock()
	cooldownUntil, inCooldown := p.cooldownUntil[filename]
	p.mu.RUnlock()
	if inCooldown && now.Before(cooldownUntil) {
		return false
	}

	return true
}

func (p *FetchPolicy) ObserveRangeRead(tracker *SequentialReadTracker, off int64, n int) {
	if tracker == nil || n <= 0 {
		return
	}

	tracker.rangeRequests++
	tracker.totalBytes += int64(n)

	if !tracker.initialized {
		tracker.initialized = true
		tracker.lastEndOffset = off + int64(n)
		tracker.sequentialBytes += int64(n)
		return
	}

	delta := off - tracker.lastEndOffset
	if delta >= 0 && delta <= p.gapToleranceBytes {
		tracker.sequentialBytes += int64(n)
	} else if delta < 0 {
		tracker.backwardSeeks++
	}

	tracker.lastEndOffset = off + int64(n)
}

func isAdaptiveTargetFile(filename string) bool {
	return len(filename) >= 3 && filename[:3] == "blk"
}
