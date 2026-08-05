package jst

import "time"

// Location returns Asia/Tokyo, falling back to a fixed UTC+9 zone.
func Location() *time.Location {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		return time.FixedZone("JST", 9*60*60)
	}
	return loc
}

// Now returns the current time in JST.
func Now() time.Time {
	return time.Now().In(Location())
}

// To converts t to JST. Zero times are left unchanged.
func To(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.In(Location())
}
