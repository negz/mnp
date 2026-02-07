package output

import "fmt"

// FormatScore formats a pinball score with appropriate suffix.
func FormatScore(score float64) string {
	switch {
	case score >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", score/1_000_000_000)
	case score >= 1_000_000:
		return fmt.Sprintf("%.1fM", score/1_000_000)
	case score >= 1_000:
		return fmt.Sprintf("%.1fK", score/1_000)
	default:
		return fmt.Sprintf("%.0f", score)
	}
}
