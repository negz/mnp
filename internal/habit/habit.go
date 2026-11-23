// Package habit models how captains choose who to send to each game when
// responding to the other team's picks.
//
// Captains mostly send whoever is most familiar with each machine, rather than
// whoever the points model says would score best there. The model gives each
// player a utility for each game they could be sent to, and predicts that a
// team sends whoever has the most utility in total. A team is likelier to
// arrange its players one way than another the more utility that arrangement
// has, and swapping two players between games that costs one unit of utility
// makes the swap about e (2.7) times less likely.
package habit

import "math"

// Features are what's known about sending a player to a game.
type Features struct {
	// IPR is the player's IPR, and Facing the average IPR of the players
	// they'd face.
	IPR    int
	Facing float64

	// Games is how many league games the player has played on the machine,
	// anywhere, and VenueGames how many at this venue.
	Games      int
	VenueGames int

	// Affinity is how much better, in log terms, the player scores on the
	// machine than they usually do relative to the league. It's zero if
	// they've never played it.
	Affinity float64

	// Win is the points model's chance that the player beats each player
	// they'd face, averaged across them.
	Win float64
}

// A Model weighs the features of sending a player to a game.
type Model struct {
	IPRMatch   float64 // Per unit of the player's IPR times the IPR they'd face.
	Games      float64 // Per unit of log(1 + games on the machine).
	VenueGames float64 // Per unit of log(1 + games on the machine at this venue).
	Affinity   float64 // Per unit of affinity.
	Win        float64 // Per unit of win chance, from 0 to 1.
}

// Singles returns the model of how captains respond in singles rounds. It and
// [Doubles] were fitted by `mnp backtest --habits` on seasons 16 to 22, then
// rounded. On seasons 23 and 24, which they weren't fitted on, they put 37% of
// singles players and 48% of doubles players on the game they were actually
// sent to. The points model's best response got 29% and 40%, and sending
// players at random gets 14% and 25%.
//
// Familiarity matters most. All else being equal, a singles player with four
// times the games on one machine as on another is about 3.5 times likelier to
// be sent to the first. A better chance of winning, and IPR matching IPR,
// matter much less.
func Singles() Model {
	return Model{IPRMatch: 0.02, Games: 0.9, VenueGames: 0.26, Affinity: 0.12, Win: 2.1}
}

// Doubles returns the model of how captains respond in doubles rounds. See
// [Singles].
func Doubles() Model {
	return Model{IPRMatch: 0.017, Games: 0.79, VenueGames: 0.39, Affinity: 0.09, Win: 0.83}
}

// Utility returns how much a captain wants to send a player to a game.
func (m Model) Utility(f Features) float64 {
	return m.IPRMatch*float64(f.IPR)*f.Facing +
		m.Games*math.Log1p(float64(f.Games)) +
		m.VenueGames*math.Log1p(float64(f.VenueGames)) +
		m.Affinity*f.Affinity +
		m.Win*f.Win
}
