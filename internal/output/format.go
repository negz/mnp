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

// FormatP75 formats a P75 score with relative strength annotation. The
// leagueP75 is the league-wide P75 for the same machine. If leagueP75 is zero
// (no league data), the annotation is omitted.
func FormatP75(p75, leagueP75 float64) string {
	score := FormatScore(p75)
	if leagueP75 == 0 {
		return score
	}
	return score + " " + FormatRelStr(p75, leagueP75)
}

// FormatRelStr formats relative strength as a percentage vs league P75.
func FormatRelStr(p75, leagueP75 float64) string {
	if leagueP75 == 0 {
		return ""
	}
	pct := (p75 - leagueP75) / leagueP75 * 100
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

// RelStr computes relative strength as a percentage vs league P75.
func RelStr(p75, leagueP75 float64) float64 {
	if leagueP75 == 0 {
		return 0
	}
	return (p75 - leagueP75) / leagueP75 * 100
}
