package mnp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/negz/mnp/internal/db"
)

// A Store loads MNP data.
type Store interface { //nolint:interfacebloat // Maps 1:1 to the store operations the ETL performs.
	UpsertMachine(ctx context.Context, m db.Machine) error
	UpsertVenue(ctx context.Context, key, name string) (int64, error)
	UpsertVenueMachine(ctx context.Context, venueID int64, machineKey string) error
	UpsertSeason(ctx context.Context, number int) (int64, error)
	UpsertTeam(ctx context.Context, t db.Team) (int64, error)
	UpsertPlayer(ctx context.Context, name string) (int64, error)
	UpsertRoster(ctx context.Context, playerID, teamID int64, role string) error
	UpsertMatch(ctx context.Context, m db.Match) (int64, error)
	InsertGame(ctx context.Context, g db.Game) (int64, error)
	InsertGameResult(ctx context.Context, r db.GameResult) error
	DeleteMatchGames(ctx context.Context, matchID int64) error
	GetTeamID(ctx context.Context, key string, seasonID int64) (int64, error)
	ListMachineKeys(ctx context.Context) (map[string]bool, error)
	LoadedSeasons(ctx context.Context) (map[int]bool, error)
}

// Machines extracts, transforms, and loads pinball machine data.
type Machines struct {
	raw map[string]machineJSON
}

type machineJSON struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// Extract reads and decodes machine data from a JSON file.
func (m *Machines) Extract(path string) error {
	return decodeJSONFile(path, &m.raw)
}

// Transform returns clean domain types from the raw JSON data.
func (m *Machines) Transform() []db.Machine {
	out := make([]db.Machine, 0, len(m.raw))
	for _, mj := range m.raw {
		out = append(out, db.Machine{Key: mj.Key, Name: mj.Name})
	}
	return out
}

// Load inserts the transformed machine data into the store.
func (m *Machines) Load(ctx context.Context, s Store) error {
	for _, machine := range m.Transform() {
		if err := s.UpsertMachine(ctx, machine); err != nil {
			return fmt.Errorf("upsert machine %s: %w", machine.Key, err)
		}
	}
	return nil
}

// Venues extracts, transforms, and loads venue data.
type Venues struct {
	raw map[string]venueRawJSON
}

type venueRawJSON struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	Machines []string `json:"machines"`
}

// VenueData is a transformed venue with its machine list.
type VenueData struct {
	Key      string
	Name     string
	Machines []string
}

// Extract reads and decodes venue data from a JSON file.
func (v *Venues) Extract(path string) error {
	return decodeJSONFile(path, &v.raw)
}

// Transform returns clean domain types from the raw JSON data.
func (v *Venues) Transform() []VenueData {
	out := make([]VenueData, 0, len(v.raw))
	for _, vj := range v.raw {
		out = append(out, VenueData(vj))
	}
	return out
}

// Load inserts the transformed venue data into the store. It skips machine
// associations for machines not already known to the store.
func (v *Venues) Load(ctx context.Context, s Store) error {
	knownMachines, err := s.ListMachineKeys(ctx)
	if err != nil {
		return fmt.Errorf("list machine keys: %w", err)
	}

	for _, vd := range v.Transform() {
		venueID, err := s.UpsertVenue(ctx, vd.Key, vd.Name)
		if err != nil {
			return fmt.Errorf("upsert venue %s: %w", vd.Key, err)
		}
		for _, mk := range vd.Machines {
			if !knownMachines[mk] {
				continue
			}
			if err := s.UpsertVenueMachine(ctx, venueID, mk); err != nil {
				return fmt.Errorf("upsert venue machine %s/%s: %w", vd.Key, mk, err)
			}
		}
	}

	return nil
}

// Season extracts, transforms, and loads season data (teams and rosters).
type Season struct {
	raw seasonRawJSON
}

type seasonRawJSON struct {
	Teams map[string]teamSeasonJSON `json:"teams"`
}

type teamSeasonJSON struct {
	Key    string `json:"key"`
	Venue  string `json:"venue"`
	Name   string `json:"name"`
	Roster []struct {
		Name string `json:"name"`
	} `json:"roster"`
}

// TeamData is a transformed team with its roster.
type TeamData struct {
	Key    string
	Name   string
	Venue  string
	Roster []string
}

// Extract reads and decodes season data from a JSON file.
func (s *Season) Extract(path string) error {
	return decodeJSONFile(path, &s.raw)
}

// Transform returns clean domain types from the raw JSON data.
func (s *Season) Transform() []TeamData {
	out := make([]TeamData, 0, len(s.raw.Teams))
	for _, t := range s.raw.Teams {
		td := TeamData{
			Key:    t.Key,
			Name:   t.Name,
			Venue:  t.Venue,
			Roster: make([]string, 0, len(t.Roster)),
		}
		for _, p := range t.Roster {
			td.Roster = append(td.Roster, p.Name)
		}
		out = append(out, td)
	}
	return out
}

// Load inserts the transformed season data into the store. It returns the
// season's database ID.
func (s *Season) Load(ctx context.Context, st Store, seasonNum int) (int64, error) {
	seasonID, err := st.UpsertSeason(ctx, seasonNum)
	if err != nil {
		return 0, fmt.Errorf("upsert season %d: %w", seasonNum, err)
	}

	for _, t := range s.Transform() {
		var venueID int64
		if t.Venue != "" {
			var err error
			venueID, err = st.UpsertVenue(ctx, t.Venue, t.Venue)
			if err != nil {
				return 0, fmt.Errorf("upsert venue %s: %w", t.Venue, err)
			}
		}

		teamID, err := st.UpsertTeam(ctx, db.Team{
			Key:         t.Key,
			Name:        t.Name,
			SeasonID:    seasonID,
			HomeVenueID: venueID,
		})
		if err != nil {
			return 0, fmt.Errorf("upsert team %s: %w", t.Key, err)
		}

		for _, name := range t.Roster {
			playerID, err := st.UpsertPlayer(ctx, name)
			if err != nil {
				return 0, fmt.Errorf("upsert player %s: %w", name, err)
			}
			if err := st.UpsertRoster(ctx, playerID, teamID, RolePlayer); err != nil {
				return 0, fmt.Errorf("upsert roster %s: %w", name, err)
			}
		}
	}

	return seasonID, nil
}

// Schedule extracts, transforms, and loads the season schedule from
// season.json's weeks array. This creates match stubs for future weeks
// that don't yet have individual match files in the archive.
type Schedule struct {
	raw scheduleRawJSON
}

type scheduleRawJSON struct {
	Weeks []weekRawJSON `json:"weeks"`
}

type weekRawJSON struct {
	N       string          `json:"n"`
	Date    string          `json:"date"`
	Matches []weekMatchJSON `json:"matches"`
}

type weekMatchJSON struct {
	MatchKey string       `json:"match_key"`
	AwayKey  string       `json:"away_key"`
	HomeKey  string       `json:"home_key"`
	Venue    venueRefJSON `json:"venue"`
}

// ScheduleMatchData is a transformed schedule entry ready for loading.
type ScheduleMatchData struct {
	Key     string
	Week    int
	Date    string
	HomeKey string
	AwayKey string
	Venue   VenueRef
}

// Extract reads and decodes schedule data from a season.json file.
func (s *Schedule) Extract(path string) error {
	return decodeJSONFile(path, &s.raw)
}

// Transform returns clean schedule entries from the raw JSON data.
func (s *Schedule) Transform() []ScheduleMatchData {
	var out []ScheduleMatchData
	for _, w := range s.raw.Weeks {
		weekNum, _ := strconv.Atoi(w.N)
		date := isoDate(w.Date)
		for _, m := range w.Matches {
			out = append(out, ScheduleMatchData{
				Key:     m.MatchKey,
				Week:    weekNum,
				Date:    date,
				HomeKey: m.HomeKey,
				AwayKey: m.AwayKey,
				Venue:   VenueRef{Key: m.Venue.Key, Name: m.Venue.Name},
			})
		}
	}
	return out
}

// Load inserts the transformed schedule data into the store as match stubs.
func (s *Schedule) Load(ctx context.Context, st Store, seasonID int64) error {
	for _, m := range s.Transform() {
		var venueID int64
		if m.Venue.Key != "" {
			var err error
			venueID, err = st.UpsertVenue(ctx, m.Venue.Key, m.Venue.Name)
			if err != nil {
				return fmt.Errorf("upsert venue %s: %w", m.Venue.Key, err)
			}
		}

		homeTeamID, err := st.GetTeamID(ctx, m.HomeKey, seasonID)
		if err != nil {
			return fmt.Errorf("get home team %s: %w", m.HomeKey, err)
		}
		awayTeamID, err := st.GetTeamID(ctx, m.AwayKey, seasonID)
		if err != nil {
			return fmt.Errorf("get away team %s: %w", m.AwayKey, err)
		}

		if _, err := st.UpsertMatch(ctx, db.Match{
			Key:        m.Key,
			SeasonID:   seasonID,
			Week:       m.Week,
			Date:       m.Date,
			HomeTeamID: homeTeamID,
			AwayTeamID: awayTeamID,
			VenueID:    venueID,
		}); err != nil {
			return fmt.Errorf("upsert schedule match %s: %w", m.Key, err)
		}
	}
	return nil
}

// Match extracts, transforms, and loads match data.
type Match struct {
	raw matchRawJSON
}

type matchRawJSON struct {
	Key    string        `json:"key"`
	Week   string        `json:"week"`
	Date   string        `json:"date"`
	State  string        `json:"state"`
	Venue  venueRefJSON  `json:"venue"`
	Home   teamMatchJSON `json:"home"`
	Away   teamMatchJSON `json:"away"`
	Rounds []roundJSON   `json:"rounds"`
}

type venueRefJSON struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type teamMatchJSON struct {
	Key    string       `json:"key"`
	Name   string       `json:"name"`
	Points int          `json:"points"`
	Lineup []lineupJSON `json:"lineup"`
}

type lineupJSON struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type roundJSON struct {
	N     int        `json:"n"`
	Games []gameJSON `json:"games"`
}

type gameJSON struct {
	N       int     `json:"n"`
	Machine string  `json:"machine"`
	Done    bool    `json:"done"`
	Player1 string  `json:"player_1"`
	Player2 string  `json:"player_2"`
	Player3 string  `json:"player_3"`
	Player4 string  `json:"player_4"`
	Score1  int64   `json:"score_1"`
	Score2  int64   `json:"score_2"`
	Score3  int64   `json:"score_3"`
	Score4  int64   `json:"score_4"`
	Points1 float64 `json:"points_1"`
	Points2 float64 `json:"points_2"`
	Points3 float64 `json:"points_3"`
	Points4 float64 `json:"points_4"`
}

// MatchData is a transformed match ready for loading.
type MatchData struct {
	Key     string
	Week    int
	Date    string
	Venue   VenueRef
	HomeKey string
	AwayKey string
	Games   []GameData
}

// VenueRef is a reference to a venue by key and name.
type VenueRef struct {
	Key  string
	Name string
}

// GameData is a transformed game within a match.
type GameData struct {
	Round      int
	MachineKey string
	IsDoubles  bool
	Results    []ResultData
}

// ResultData is a transformed player result within a game.
type ResultData struct {
	PlayerName string
	Score      int64
	Position   int
	IsHome     bool
}

// Extract reads and decodes match data from a match file.
func (m *Match) Extract(path string) error {
	return decodeJSONFile(path, &m.raw)
}

// decodeJSONFile opens a file and decodes its JSON contents into v.
func decodeJSONFile(path string, v any) error {
	f, err := os.Open(path) //nolint:gosec // Internal archive path.
	if err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(path), err)
	}
	defer f.Close() //nolint:errcheck // Read-only file.
	return json.NewDecoder(f).Decode(v)
}

// Transform resolves player hashes to names and builds clean game results.
func (m *Match) Transform() MatchData {
	playerNames := make(map[string]string)
	for _, p := range m.raw.Home.Lineup {
		playerNames[p.Key] = p.Name
	}
	for _, p := range m.raw.Away.Lineup {
		playerNames[p.Key] = p.Name
	}

	weekNum, _ := strconv.Atoi(m.raw.Week)

	var games []GameData
	for _, r := range m.raw.Rounds {
		isDoubles := r.N == 1 || r.N == 4
		for _, g := range r.Games {
			if !g.Done {
				continue
			}
			games = append(games, GameData{
				Round:      r.N,
				MachineKey: g.Machine,
				IsDoubles:  isDoubles,
				Results:    buildResults(g, playerNames, isDoubles),
			})
		}
	}

	return MatchData{
		Key:     m.raw.Key,
		Week:    weekNum,
		Date:    isoDate(m.raw.Date),
		Venue:   VenueRef{Key: m.raw.Venue.Key, Name: m.raw.Venue.Name},
		HomeKey: m.raw.Home.Key,
		AwayKey: m.raw.Away.Key,
		Games:   games,
	}
}

// buildResults constructs player results for a game, resolving hashes to names.
// In doubles (rounds 1,4): players 1,3 are away team, 2,4 are home team.
// In singles (rounds 2,3): player 1 is away, player 2 is home.
func buildResults(g gameJSON, playerNames map[string]string, isDoubles bool) []ResultData {
	type raw struct {
		hash   string
		score  int64
		pos    int
		isHome bool
	}

	raws := []raw{
		{hash: g.Player1, score: g.Score1, pos: 1, isHome: false},
		{hash: g.Player2, score: g.Score2, pos: 2, isHome: true},
	}
	if isDoubles {
		raws = append(raws,
			raw{hash: g.Player3, score: g.Score3, pos: 3, isHome: false},
			raw{hash: g.Player4, score: g.Score4, pos: 4, isHome: true},
		)
	}

	results := make([]ResultData, 0, len(raws))
	for _, r := range raws {
		if r.hash == "" {
			continue
		}
		name, ok := playerNames[r.hash]
		if !ok {
			continue
		}
		results = append(results, ResultData{
			PlayerName: name,
			Score:      r.score,
			Position:   r.pos,
			IsHome:     r.isHome,
		})
	}
	return results
}

func isoDate(s string) string {
	t, err := time.Parse("01/02/2006", s)
	if err != nil {
		return s
	}
	return t.Format("2006-01-02")
}

// Load inserts the transformed match data into the store.
func (m *Match) Load(ctx context.Context, s Store, seasonID int64) error {
	data := m.Transform()

	var venueID int64
	if data.Venue.Key != "" {
		var err error
		venueID, err = s.UpsertVenue(ctx, data.Venue.Key, data.Venue.Name)
		if err != nil {
			return fmt.Errorf("upsert venue %s: %w", data.Venue.Key, err)
		}
	}

	homeTeamID, err := s.GetTeamID(ctx, data.HomeKey, seasonID)
	if err != nil {
		return fmt.Errorf("get home team: %w", err)
	}
	awayTeamID, err := s.GetTeamID(ctx, data.AwayKey, seasonID)
	if err != nil {
		return fmt.Errorf("get away team: %w", err)
	}

	matchID, err := s.UpsertMatch(ctx, db.Match{
		Key:        data.Key,
		SeasonID:   seasonID,
		Week:       data.Week,
		Date:       data.Date,
		HomeTeamID: homeTeamID,
		AwayTeamID: awayTeamID,
		VenueID:    venueID,
	})
	if err != nil {
		return fmt.Errorf("upsert match %s: %w", data.Key, err)
	}

	if err := s.DeleteMatchGames(ctx, matchID); err != nil {
		return fmt.Errorf("delete match games %s: %w", data.Key, err)
	}

	for _, g := range data.Games {
		gameID, err := s.InsertGame(ctx, db.Game{
			MatchID:    matchID,
			Round:      g.Round,
			MachineKey: g.MachineKey,
			IsDoubles:  g.IsDoubles,
		})
		if err != nil {
			return fmt.Errorf("insert game: %w", err)
		}

		for _, r := range g.Results {
			playerID, err := s.UpsertPlayer(ctx, r.PlayerName)
			if err != nil {
				return fmt.Errorf("upsert player %s: %w", r.PlayerName, err)
			}

			teamID := awayTeamID
			if r.IsHome {
				teamID = homeTeamID
			}

			if err := s.InsertGameResult(ctx, db.GameResult{
				GameID:   gameID,
				PlayerID: playerID,
				TeamID:   teamID,
				Position: r.Position,
				Score:    r.Score,
			}); err != nil {
				return fmt.Errorf("insert game result: %w", err)
			}
		}
	}

	return nil
}
