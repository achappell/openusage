package copilot

import (
	"testing"
	"time"
)

func TestTodaySeriesValue_PresentReturnsValue(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	m := map[string]float64{today: 42.0}
	date, v := todaySeriesValue(m)
	if date != today || v != 42.0 {
		t.Errorf("todaySeriesValue with today's entry = (%q, %v), want (%q, 42.0)", date, v, today)
	}
}

func TestTodaySeriesValue_AbsentReturnsZero(t *testing.T) {
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	lastWeek := time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	m := map[string]float64{yesterday: 100.0, lastWeek: 500.0}
	date, v := todaySeriesValue(m)
	if date != "" || v != 0 {
		t.Errorf("todaySeriesValue with no today entry = (%q, %v), want (\"\", 0)", date, v)
	}
}

func TestTodaySeriesValue_EmptyMapReturnsZero(t *testing.T) {
	date, v := todaySeriesValue(nil)
	if date != "" || v != 0 {
		t.Errorf("todaySeriesValue on nil map = (%q, %v), want (\"\", 0)", date, v)
	}
}
