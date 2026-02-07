package output

import (
	"fmt"
	"math"
)

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

// FormatP50 formats a P50 score with relative strength annotation. The
// leagueP50 is the league-wide P50 for the same machine. If leagueP50 is zero
// (no league data), the annotation is omitted.
func FormatP50(p50, leagueP50 float64) string {
	score := FormatScore(p50)
	if leagueP50 == 0 {
		return score
	}
	return score + " " + FormatRelStr(p50, leagueP50)
}

// FormatRelStr formats relative strength as a percentage vs league P50.
func FormatRelStr(p50, leagueP50 float64) string {
	if leagueP50 == 0 {
		return ""
	}
	pct := (p50 - leagueP50) / leagueP50 * 100
	rounded := int(math.Round(pct))
	switch {
	case rounded > 0:
		return fmt.Sprintf("(+%d%%)", rounded)
	case rounded < 0:
		return fmt.Sprintf("(%d%%)", rounded)
	default:
		return "(avg)"
	}
}

// RelStr computes relative strength as a percentage vs league P50.
func RelStr(p50, leagueP50 float64) float64 {
	if leagueP50 == 0 {
		return 0
	}
	return (p50 - leagueP50) / leagueP50 * 100
}
