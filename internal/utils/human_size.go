package utils

import "fmt"

// HumanFileSize formats a byte count as a human-readable size string.
func HumanFileSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(1), 0
	for m := n; m >= unit && exp < 3; m /= unit {
		div *= unit
		exp++
	}
	switch exp {
	case 1:
		return fmt.Sprintf("%.0f KiB", float64(n)/float64(div))
	case 2:
		return fmt.Sprintf("%.1f MiB", float64(n)/float64(div))
	default:
		return fmt.Sprintf("%.2f GiB", float64(n)/float64(div))
	}
}
