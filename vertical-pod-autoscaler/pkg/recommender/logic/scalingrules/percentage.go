package scalingrules

import (
	"fmt"
	"strconv"
	"strings"
)

// parsePercentage converts a non-negative percentage string such as "150%" to its multiplier (1.5).
func parsePercentage(value, field string) (float64, error) {
	if value == "" {
		return 0, fmt.Errorf("missing %q", field)
	}
	if !strings.HasSuffix(value, "%") {
		return 0, fmt.Errorf("%q must be a percentage", field)
	}
	percentage, err := strconv.ParseFloat(strings.TrimSuffix(value, "%"), 64)
	if err != nil || percentage < 0 {
		return 0, fmt.Errorf("invalid %q percentage %q", field, value)
	}
	return percentage / 100, nil
}
