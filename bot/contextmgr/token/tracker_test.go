package token

import "testing"

func TestTracker_RecordUsage(t *testing.T) {
	var tr Tracker
	tr.RecordUsage(1000, 500)
	if tr.PromptEstimate() <= 0 {
		t.Error("after RecordUsage, PromptEstimate should use API data")
	}
}

func TestTracker_AddNew(t *testing.T) {
	var tr Tracker
	tr.RecordUsage(1000, 500) // calibrate
	tr.AddNew(400)            // ~100 new tokens
	if tr.PromptEstimate() <= 1000 {
		t.Error("AddNew should increase estimate beyond baseline")
	}
}

func TestTracker_ResetOnRecord(t *testing.T) {
	var tr Tracker
	tr.RecordUsage(100, 50)
	tr.AddNew(1000) // add pending tokens
	estBefore := tr.PromptEstimate()
	tr.RecordUsage(200, 80) // new API call resets pending
	if tr.PromptEstimate() >= estBefore {
		t.Error("new RecordUsage should reset newMessageTokens, lowering estimate")
	}
}

func TestTracker_CacheStats(t *testing.T) {
	var tr Tracker
	h, m := tr.CacheStats()
	if h != 0 || m != 0 {
		t.Error("initial cache stats should be zero")
	}
	tr.RecordCache(100, 50)
	h, m = tr.CacheStats()
	if h != 100 || m != 50 {
		t.Errorf("after record: hit=%d miss=%d, want 100/50", h, m)
	}
}

func TestTracker_CacheHitRatio(t *testing.T) {
	var tr Tracker
	if r := tr.CacheHitRatio(); r != 0 {
		t.Error("initial ratio should be 0")
	}
	tr.RecordCache(75, 25)
	if r := tr.CacheHitRatio(); r != 0.75 {
		t.Errorf("ratio = %f, want 0.75", r)
	}
}

func TestTracker_NoAPIData(t *testing.T) {
	var tr Tracker
	if tr.PromptEstimate() != 0 {
		t.Error("without API data, PromptEstimate should be 0")
	}
	if tr.Total() != 0 {
		t.Error("without API data, Total should be 0")
	}
}
