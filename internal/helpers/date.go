package helpers

import (
	"fmt"
	"time"
)

// FormatDateString attempts to parse a date string using several known layouts
// (e.g. "20060102150405" for deployment IDs or RFC3339 "2006-01-02T15:04:05Z")
// and returns a human-readable string. For events that happened today it displays
// "today at HH:MM", for yesterday it shows "yesterday at HH:MM", for events within
// the last 24 to 48 hours it shows relative time in days, and for events older than
// two days, it shows an absolute date and time.
func FormatDateString(dateString string) (string, error) {
	var t time.Time
	var err error

	switch len(dateString) {
	case 14:
		t, err = time.Parse("20060102150405", dateString)
	case 16: // with centiseconds
		t, err = time.Parse("20060102150405", dateString[:14])
	default:
		// Try RFC3339 and other formats
		layouts := []string{time.RFC3339, time.RFC3339Nano}
		for _, layout := range layouts {
			t, err = time.Parse(layout, dateString)
			if err == nil {
				break
			}
		}
	}

	if err != nil {
		return "", fmt.Errorf("failed to parse date string %q: %w", dateString, err)
	}

	// Convert to local time for display purposes.
	tLocal := t.Local()
	now := time.Now().Local()
	formattedTime := tLocal.Format("15:04")
	elapsed := now.Sub(tLocal)

	// Use "today" and "yesterday" labels if the dates match.
	if now.Year() == tLocal.Year() {
		ydayNow := now.YearDay()
		ydayT := tLocal.YearDay()
		switch diff := ydayNow - ydayT; diff {
		case 0:
			return fmt.Sprintf("today at %s", formattedTime), nil
		case 1:
			return fmt.Sprintf("yesterday at %s", formattedTime), nil
		}
	}

	// For events older than 48 hours (2 days), show the absolute date/time.
	if elapsed >= 48*time.Hour {
		absolute := tLocal.Format("Jan 2, 2006 at 15:04")
		return absolute, nil
	}

	// For events between 24 and 48 hours, display relative time in days.
	if elapsed >= 24*time.Hour {
		days := int(elapsed.Hours() / 24)
		return fmt.Sprintf("%d day(s) ago at %s", days, formattedTime), nil
	}

	// For recent events (within 24 hours), display relative time.
	switch {
	case elapsed < time.Minute:
		return fmt.Sprintf("%d seconds ago at %s", int(elapsed.Seconds()), formattedTime), nil
	case elapsed < time.Hour:
		return fmt.Sprintf("%d minutes ago at %s", int(elapsed.Minutes()), formattedTime), nil
	default:
		return fmt.Sprintf("%d hours ago at %s", int(elapsed.Hours()), formattedTime), nil
	}
}
