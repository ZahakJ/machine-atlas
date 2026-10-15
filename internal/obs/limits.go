package obs

import "fmt"

// Size limits on a single value. The snapshot schema enforces them on import
// (a snapshot is untrusted input) and the collection runner on emission, so a
// freshly collected snapshot always passes its own validation.
const (
	MaxStringBytes  = 4096
	MaxTableRows    = 2000
	MaxTableColumns = 64
)

// CheckLimits reports whether a normalized value respects the size limits:
// strings (including table cells and column names) of at most MaxStringBytes,
// tables of at most MaxTableRows rows and MaxTableColumns columns per row.
func CheckLimits(v any) error {
	switch x := v.(type) {
	case string:
		if len(x) > MaxStringBytes {
			return fmt.Errorf("string of %d bytes exceeds %d", len(x), MaxStringBytes)
		}
	case []map[string]any:
		if len(x) > MaxTableRows {
			return fmt.Errorf("table of %d rows exceeds %d", len(x), MaxTableRows)
		}
		for i, row := range x {
			if len(row) > MaxTableColumns {
				return fmt.Errorf("table row %d has %d columns, more than %d", i, len(row), MaxTableColumns)
			}
			for k, c := range row {
				if len(k) > MaxStringBytes {
					return fmt.Errorf("table row %d: column name exceeds %d bytes", i, MaxStringBytes)
				}
				if err := CheckLimits(c); err != nil {
					return fmt.Errorf("table row %d column %q: %w", i, k, err)
				}
			}
		}
	}
	return nil
}
