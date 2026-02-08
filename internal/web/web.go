// Package web implements the MNP web frontend.
package web

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/negz/mnp/internal/db"
	"github.com/negz/mnp/internal/output"
	"github.com/negz/mnp/internal/strategy/matchup"
	"github.com/negz/mnp/internal/strategy/player"
	"github.com/negz/mnp/internal/strategy/recommend"
	"github.com/negz/mnp/internal/strategy/scout"
)

//go:embed static
var static embed.FS

//go:embed templates
var tmpls embed.FS

// Store is the set of queries needed by the web UI. It composes the strategy
// package store interfaces with the list queries used to populate dropdowns.
type Store interface { //nolint:interfacebloat // Composes four strategy store interfaces plus list queries.
	scout.Store
	matchup.Store
	recommend.Store
	player.Store

	ListTeams(ctx context.Context, search string) ([]db.TeamSummary, error)
	ListVenues(ctx context.Context, search string) ([]db.Venue, error)
	ListMachines(ctx context.Context, search string) ([]db.Machine, error)
	ListSchedule(ctx context.Context, after string) ([]db.ScheduleMatch, error)
}

type serverTemplate struct {
	home      *template.Template
	team      *template.Template
	matchup   *template.Template
	recommend *template.Template
	scout     *template.Template
	player    *template.Template
	teams     *template.Template
}

// Server serves the MNP web UI.
type Server struct {
	store    Store
	log      *slog.Logger
	template serverTemplate
}

// NewServer returns a new Server.
func NewServer(store Store, log *slog.Logger) *Server {
	return &Server{
		store: store,
		log:   log,
		template: serverTemplate{
			home:      parseTemplates("templates/home.html"),
			team:      parseTemplates("templates/team.html"),
			matchup:   parseTemplates("templates/matchup.html"),
			recommend: parseTemplates("templates/recommend.html"),
			scout:     parseTemplates("templates/scout.html"),
			player:    parseTemplates("templates/player.html"),
			teams:     parseTemplates("templates/teams.html"),
		},
	}
}

// Handler returns an http.Handler with all routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	staticFS, _ := fs.Sub(static, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	mux.HandleFunc("GET /", s.handleHome)

	mux.HandleFunc("GET /t/{team}", s.handleTeam)

	mux.HandleFunc("GET /matchup", s.handleMatchup)

	mux.HandleFunc("GET /t/{team}/scout", s.handleScout)

	mux.HandleFunc("GET /scout", func(w http.ResponseWriter, r *http.Request) {
		team := strings.ToUpper(r.URL.Query().Get("team"))
		if team != "" {
			q := r.URL.Query()
			q.Del("team")
			path := fmt.Sprintf("/t/%s/scout", team)
			if encoded := q.Encode(); encoded != "" {
				path += "?" + encoded
			}
			http.Redirect(w, r, path, http.StatusFound)
			return
		}
		s.handleScoutForm(w, r)
	})

	mux.HandleFunc("GET /p/{name...}", s.handlePlayer)

	mux.HandleFunc("GET /t/{team}/recommend/{machine}", s.handleRecommend)

	mux.HandleFunc("GET /teams", s.handleTeams)

	mux.HandleFunc("GET /recommend", func(w http.ResponseWriter, r *http.Request) {
		team := strings.ToUpper(r.URL.Query().Get("team"))
		machine := r.URL.Query().Get("machine")
		if team != "" && machine != "" {
			q := r.URL.Query()
			q.Del("team")
			q.Del("machine")
			path := fmt.Sprintf("/t/%s/recommend/%s", team, machine)
			if encoded := q.Encode(); encoded != "" {
				path += "?" + encoded
			}
			http.Redirect(w, r, path, http.StatusFound)
			return
		}
		s.handleRecommendForm(w, r)
	})

	return mux
}

type statusRecorder struct {
	http.ResponseWriter

	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// WithLogging wraps an http.Handler to log each request's method, path,
// status code, and duration.
func WithLogging(next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Info("request", "method", r.Method, "path", r.URL.RequestURI(), "status", rec.status, "duration", time.Since(start))
	})
}

func newTemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"formatScore": func(score float64) string {
			if score == 0 {
				return "-"
			}
			return output.FormatScore(score)
		},
		"formatEdge": func(pct float64, team1, team2 string, conf matchup.Confidence) string {
			if math.IsInf(pct, 0) || pct > 1e15 || pct < -1e15 {
				if pct > 0 {
					return team1
				}
				return team2
			}
			rounded := int(math.Round(pct))
			icon := confidenceIcon(conf)
			switch {
			case rounded > 0:
				return fmt.Sprintf("%s %d%% %s", team1, rounded, icon)
			case rounded < 0:
				return fmt.Sprintf("%s %d%% %s", team2, -rounded, icon)
			default:
				return "Even"
			}
		},
		"join": strings.Join,
		"formatP50": func(p50, leagueP50 float64) string {
			if p50 == 0 {
				return "-"
			}
			return output.FormatP50(p50, leagueP50)
		},
		"formatRelStr": output.FormatRelStr,
		"shortName": func(name string) string {
			if first, last, ok := strings.Cut(name, " "); ok {
				return first + " " + last[:1]
			}
			return name
		},
		"pathEscape": url.PathEscape,
	}
}

func confidenceIcon(c matchup.Confidence) string {
	switch c {
	case matchup.ConfidenceHigh:
		return "▲"
	case matchup.ConfidenceMedium:
		return "△"
	case matchup.ConfidenceLow:
		return "▼"
	default:
		return "▼"
	}
}

func parseTemplates(pages ...string) *template.Template {
	files := append([]string{"templates/layout.html"}, pages...)
	return template.Must(template.New("layout.html").Funcs(newTemplateFuncs()).ParseFS(tmpls, files...))
}

// Sync runs a data sync using the provided function, then repeats every
// interval. It blocks until the context is cancelled.
func Sync(ctx context.Context, syncFn func(context.Context) error, interval time.Duration, log *slog.Logger) {
	if err := syncFn(ctx); err != nil {
		log.Error("initial sync failed", "err", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := syncFn(ctx); err != nil {
				log.Error("periodic sync failed", "err", err)
			}
		}
	}
}

// Home page.

type scheduleWeek struct {
	Week    int
	Date    string
	Matches []db.ScheduleMatch
}

type homeData struct {
	Weeks       []scheduleWeek
	CurrentWeek int
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	today := time.Now().Format("2006-01-02")
	matches, err := s.store.ListSchedule(r.Context(), "")
	if err != nil {
		s.log.Error("list schedule", "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	weeks := groupByWeek(matches)

	currentWeek := 0
	for _, wk := range weeks {
		if wk.Date >= today {
			currentWeek = wk.Week
			break
		}
	}
	if currentWeek == 0 && len(weeks) > 0 {
		currentWeek = weeks[len(weeks)-1].Week
	}
	if q := r.URL.Query().Get("week"); q != "" {
		if n, err := strconv.Atoi(q); err == nil {
			currentWeek = n
		}
	}

	if err := s.template.home.ExecuteTemplate(w, "layout.html", homeData{Weeks: weeks, CurrentWeek: currentWeek}); err != nil {
		s.log.Error("render template", "err", err)
	}
}

func groupByWeek(matches []db.ScheduleMatch) []scheduleWeek {
	var weeks []scheduleWeek
	for _, m := range matches {
		if len(weeks) == 0 || weeks[len(weeks)-1].Week != m.Week {
			weeks = append(weeks, scheduleWeek{Week: m.Week, Date: m.Date})
		}
		weeks[len(weeks)-1].Matches = append(weeks[len(weeks)-1].Matches, m)
	}
	return weeks
}

// Team schedule page.

type teamData struct {
	TeamKey  string
	TeamName string
	Matches  []db.ScheduleMatch
}

func (s *Server) handleTeam(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	team := strings.ToUpper(r.PathValue("team"))

	today := time.Now().Format("2006-01-02")
	matches, err := s.store.ListSchedule(ctx, today)
	if err != nil {
		s.log.Error("list schedule", "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	matches = filterMatches(matches, team)

	name := team
	teams, err := s.store.ListTeams(ctx, team)
	if err == nil {
		for _, t := range teams {
			if t.Key == team {
				name = t.Name
				break
			}
		}
	}

	if err := s.template.team.ExecuteTemplate(w, "layout.html", teamData{TeamKey: team, TeamName: name, Matches: matches}); err != nil {
		s.log.Error("render template", "err", err)
	}
}

func filterMatches(matches []db.ScheduleMatch, team string) []db.ScheduleMatch {
	var filtered []db.ScheduleMatch
	for _, m := range matches {
		if m.HomeTeamKey == team || m.AwayTeamKey == team {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

// Matchup page.

type matchupData struct {
	Venues []db.Venue
	Teams  []db.TeamSummary
	Venue  string
	Team1  string
	Team2  string
	Result *matchup.Result
	Error  string
}

func (s *Server) handleMatchup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	venues, err := s.store.ListVenues(ctx, "")
	if err != nil {
		s.log.Error("list venues", "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	teams, err := s.store.ListTeams(ctx, "")
	if err != nil {
		s.log.Error("list teams", "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	data := matchupData{
		Venues: venues,
		Teams:  teams,
		Venue:  r.URL.Query().Get("venue"),
		Team1:  r.URL.Query().Get("t1"),
		Team2:  r.URL.Query().Get("t2"),
	}

	if data.Venue != "" && data.Team1 != "" && data.Team2 != "" {
		result, err := matchup.Analyze(ctx, s.store, data.Venue, data.Team1, data.Team2)
		switch {
		case err != nil:
			data.Error = fmt.Sprintf("Error: %v", err)
		case len(result.Machines) == 0:
			data.Error = fmt.Sprintf("No machines found at %s.", data.Venue)
		default:
			data.Result = result
		}
	}

	if err := s.template.matchup.ExecuteTemplate(w, "layout.html", data); err != nil {
		s.log.Error("render template", "err", err)
	}
}

// Recommend page.

type recommendData struct {
	Teams    []db.TeamSummary
	Machines []db.Machine
	Venues   []db.Venue

	Team    string
	Machine string
	Venue   string
	Vs      string

	TeamName    string
	MachineName string

	Result *recommend.Result
	Error  string
}

func (d recommendData) FormatAssessment() string {
	a := d.Result.Assessment
	if a == nil {
		return ""
	}
	switch a.Verdict {
	case recommend.VerdictStrong:
		return fmt.Sprintf("%s outscores %s's best (%s) by ~%s P50. Strong pick.",
			a.OurBest, d.Result.Opponent, a.TheirBest, output.FormatScore(a.Diff))
	case recommend.VerdictWeak:
		return fmt.Sprintf("%s's best (%s) outscores %s by ~%s P50. Weak pick.",
			d.Result.Opponent, a.TheirBest, a.OurBest, output.FormatScore(-a.Diff))
	case recommend.VerdictContested:
		return fmt.Sprintf("%s and %s's best (%s) are roughly even. Contested.",
			a.OurBest, d.Result.Opponent, a.TheirBest)
	}
	return ""
}

func (s *Server) handleRecommendForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	teams, err := s.store.ListTeams(ctx, "")
	if err != nil {
		s.log.Error("list teams", "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	machines, err := s.store.ListMachines(ctx, "")
	if err != nil {
		s.log.Error("list machines", "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	venues, err := s.store.ListVenues(ctx, "")
	if err != nil {
		s.log.Error("list venues", "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	data := recommendData{
		Teams:    teams,
		Machines: machines,
		Venues:   venues,
		Team:     r.URL.Query().Get("team"),
		Machine:  r.URL.Query().Get("machine"),
	}

	if err := s.template.recommend.ExecuteTemplate(w, "layout.html", data); err != nil {
		s.log.Error("render template", "err", err)
	}
}

func (s *Server) handleRecommend(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	team := strings.ToUpper(r.PathValue("team"))
	machine := r.PathValue("machine")

	teams, err := s.store.ListTeams(ctx, "")
	if err != nil {
		s.log.Error("list teams", "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	machines, err := s.store.ListMachines(ctx, "")
	if err != nil {
		s.log.Error("list machines", "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	venues, err := s.store.ListVenues(ctx, "")
	if err != nil {
		s.log.Error("list venues", "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	venue := r.URL.Query().Get("venue")
	vs := r.URL.Query().Get("vs")

	data := recommendData{
		Teams:    teams,
		Machines: machines,
		Venues:   venues,
		Team:     team,
		Machine:  machine,
		Venue:    venue,
		Vs:       vs,
	}

	for _, t := range teams {
		if t.Key == team {
			data.TeamName = t.Name
			break
		}
	}
	for _, m := range machines {
		if m.Key == machine {
			data.MachineName = m.Name
			break
		}
	}

	var opts []recommend.Option
	switch {
	case vs != "":
		opts = append(opts, recommend.VsOpponent(vs))
	case venue != "":
		opts = append(opts, recommend.AtVenue(venue))
	}

	result, err := recommend.Analyze(ctx, s.store, team, machine, opts...)
	switch {
	case err != nil:
		data.Error = fmt.Sprintf("Error: %v", err)
	case len(result.GlobalStats) == 0 && len(result.VenueStats) == 0 && len(result.OpponentStats) == 0:
		data.Error = fmt.Sprintf("No data for %s on %s.", team, machine)
	default:
		data.Result = result
	}

	if err := s.template.recommend.ExecuteTemplate(w, "layout.html", data); err != nil {
		s.log.Error("render template", "err", err)
	}
}

// Scout page.

type scoutData struct {
	Teams  []db.TeamSummary
	Venues []db.Venue

	Team     string
	Venue    string
	TeamName string

	Result *scout.Result
	Error  string
}

func (s *Server) handleScoutForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	teams, err := s.store.ListTeams(ctx, "")
	if err != nil {
		s.log.Error("list teams", "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	venues, err := s.store.ListVenues(ctx, "")
	if err != nil {
		s.log.Error("list venues", "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	data := scoutData{
		Teams:  teams,
		Venues: venues,
	}

	if err := s.template.scout.ExecuteTemplate(w, "layout.html", data); err != nil {
		s.log.Error("render template", "err", err)
	}
}

func (s *Server) handleScout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	team := strings.ToUpper(r.PathValue("team"))

	teams, err := s.store.ListTeams(ctx, "")
	if err != nil {
		s.log.Error("list teams", "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	venues, err := s.store.ListVenues(ctx, "")
	if err != nil {
		s.log.Error("list venues", "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	venue := r.URL.Query().Get("venue")

	data := scoutData{
		Teams:  teams,
		Venues: venues,
		Team:   team,
		Venue:  venue,
	}

	for _, t := range teams {
		if t.Key == team {
			data.TeamName = t.Name
			break
		}
	}

	var opts []scout.Option
	if venue != "" {
		opts = append(opts, scout.AtVenue(venue))
	}

	result, err := scout.Analyze(ctx, s.store, team, opts...)
	switch {
	case err != nil:
		data.Error = fmt.Sprintf("Error: %v", err)
	case len(result.GlobalStats) == 0:
		data.Error = fmt.Sprintf("No data for %s.", team)
	default:
		data.Result = result
	}

	if err := s.template.scout.ExecuteTemplate(w, "layout.html", data); err != nil {
		s.log.Error("render template", "err", err)
	}
}

// Player page.

type playerData struct {
	Name   string
	Result *player.Result
	Error  string
}

func (s *Server) handlePlayer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")

	data := playerData{
		Name: name,
	}

	result, err := player.Analyze(ctx, s.store, name)
	switch {
	case err != nil:
		data.Error = fmt.Sprintf("Error: %v", err)
	case len(result.GlobalStats) == 0:
		data.Error = fmt.Sprintf("No data for %s.", name)
	default:
		data.Result = result
	}

	if err := s.template.player.ExecuteTemplate(w, "layout.html", data); err != nil {
		s.log.Error("render template", "err", err)
	}
}

// Teams page.

type teamsData struct {
	Teams []db.TeamSummary
}

func (s *Server) handleTeams(w http.ResponseWriter, r *http.Request) {
	teams, err := s.store.ListTeams(r.Context(), "")
	if err != nil {
		s.log.Error("list teams", "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := s.template.teams.ExecuteTemplate(w, "layout.html", teamsData{Teams: teams}); err != nil {
		s.log.Error("render template", "err", err)
	}
}
