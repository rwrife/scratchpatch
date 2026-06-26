package index

import (
	"encoding/json"
	"fmt"
	"time"
)

// Duration is a time.Duration that marshals to/from a JSON string using Go's
// duration syntax (e.g. "168h0m0s"). This keeps the index human-readable
// instead of dumping raw nanosecond integers, and round-trips losslessly.
//
// Human-friendly *input* parsing ("7d", "30m") lives in the ttl package
// (M5); this type is purely about on-disk representation.
type Duration time.Duration

// MarshalJSON renders the duration as a quoted Go duration string.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// UnmarshalJSON accepts either a duration string ("168h0m0s") or a raw
// number of nanoseconds, so older/raw indexes still load.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch val := v.(type) {
	case string:
		parsed, err := time.ParseDuration(val)
		if err != nil {
			return fmt.Errorf("parse duration %q: %w", val, err)
		}
		*d = Duration(parsed)
		return nil
	case float64:
		*d = Duration(time.Duration(val))
		return nil
	default:
		return fmt.Errorf("invalid duration value: %v", v)
	}
}

// Duration returns the value as a standard time.Duration.
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// String renders the Go duration string.
func (d Duration) String() string { return time.Duration(d).String() }
