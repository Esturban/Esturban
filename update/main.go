package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

const (
	readmePath  = "../README.md"
	profilePath = "profile.json"
	statePath   = "ai_radar_state.json"
)

type profileConfig struct {
	Name            string        `json:"name"`
	PortfolioURL    string        `json:"portfolioUrl"`
	PortfolioLabel  string        `json:"portfolioLabel"`
	LinkedInURL     string        `json:"linkedInUrl"`
	ResumeURL       string        `json:"resumeUrl"`
	GitHubURL       string        `json:"githubUrl"`
	Hero            heroSection   `json:"hero"`
	Now             []string      `json:"now"`
	ActiveBuilds    []projectCard `json:"activeBuilds"`
	OpenSourceTools []projectCard `json:"openSourceTools"`
	PrivateSystems  []privateCard `json:"privateSystems"`
	WorkWithMe      ctaBlock      `json:"workWithMe"`
	Contribute      ctaBlock      `json:"contribute"`
	AIRadar         aiRadarConfig `json:"aiRadar"`
}

type heroSection struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type projectCard struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Summary string `json:"summary"`
}

type privateCard struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

type ctaBlock struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type aiRadarConfig struct {
	Enabled           bool         `json:"enabled"`
	MaxItems          int          `json:"maxItems"`
	RecentWindowHours int          `json:"recentWindowHours"`
	Sources           []feedSource `json:"sources"`
	FallbackItems     []radarItem  `json:"fallbackItems"`
}

type feedSource struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type radarState struct {
	Recent    []seenItem  `json:"recent"`
	LastItems []radarItem `json:"lastItems"`
}

type seenItem struct {
	URL    string `json:"url"`
	SeenAt string `json:"seenAt"`
}

type radarItem struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Source      string `json:"source"`
	Summary     string `json:"summary,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
}

func main() {
	profile, err := loadProfile(profilePath)
	if err != nil {
		failf("load profile: %v", err)
	}

	state, err := loadState(statePath)
	if err != nil {
		failf("load state: %v", err)
	}

	now := time.Now().UTC()
	selected, nextState := buildAIRadar(profile.AIRadar, state, now)
	readme := renderReadme(profile, selected)

	if err := writeIfChanged(readmePath, []byte(readme)); err != nil {
		failf("write readme: %v", err)
	}

	stateBytes, err := json.MarshalIndent(nextState, "", "  ")
	if err != nil {
		failf("marshal state: %v", err)
	}
	stateBytes = append(stateBytes, '\n')
	if err := writeIfChanged(statePath, stateBytes); err != nil {
		failf("write state: %v", err)
	}
}

func loadProfile(path string) (profileConfig, error) {
	var cfg profileConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if cfg.AIRadar.MaxItems <= 0 {
		cfg.AIRadar.MaxItems = 3
	}
	if cfg.AIRadar.RecentWindowHours <= 0 {
		cfg.AIRadar.RecentWindowHours = 24 * 7
	}
	return cfg, nil
}

func loadState(path string) (radarState, error) {
	var state radarState
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	return state, nil
}

func buildAIRadar(cfg aiRadarConfig, state radarState, now time.Time) ([]radarItem, radarState) {
	if !cfg.Enabled {
		return nil, state
	}

	candidates := fetchFeedItems(cfg.Sources)
	primary := selectTopItems(candidates, cfg, now, nil)
	selected := primary
	recentURLs := recentURLMap(state, time.Duration(cfg.RecentWindowHours)*time.Hour, now)
	if !sameItems(primary, state.LastItems) && len(recentURLs) > 0 {
		deduped := selectTopItems(candidates, cfg, now, recentURLs)
		if len(deduped) == cfg.MaxItems {
			selected = deduped
		}
	}
	if len(selected) == 0 {
		selected = cloneItems(state.LastItems)
	}
	if len(selected) == 0 {
		selected = cloneItems(cfg.FallbackItems)
	}
	if len(selected) > cfg.MaxItems {
		selected = selected[:cfg.MaxItems]
	}

	nextState := pruneState(state, time.Duration(cfg.RecentWindowHours)*time.Hour, now)
	if sameItems(selected, state.LastItems) {
		return selected, nextState
	}

	nextState.LastItems = cloneItems(selected)
	for _, item := range selected {
		if item.URL == "" {
			continue
		}
		nextState.Recent = append(nextState.Recent, seenItem{
			URL:    canonicalURL(item.URL),
			SeenAt: now.Format(time.RFC3339),
		})
	}
	nextState = pruneState(nextState, time.Duration(cfg.RecentWindowHours)*time.Hour, now)
	return selected, nextState
}

func fetchFeedItems(sources []feedSource) []radarItem {
	parser := gofeed.NewParser()
	parser.Client = &http.Client{
		Timeout: 18 * time.Second,
	}

	var items []radarItem
	for _, source := range sources {
		feed, err := parser.ParseURL(source.URL)
		if err != nil {
			continue
		}
		for _, entry := range feed.Items {
			if entry == nil {
				continue
			}
			title := strings.TrimSpace(entry.Title)
			link := strings.TrimSpace(entry.Link)
			if title == "" || link == "" {
				continue
			}
			title = strings.TrimPrefix(title, "[AINews] ")
			publishedAt := ""
			switch {
			case entry.PublishedParsed != nil:
				publishedAt = entry.PublishedParsed.UTC().Format(time.RFC3339)
			case entry.UpdatedParsed != nil:
				publishedAt = entry.UpdatedParsed.UTC().Format(time.RFC3339)
			}
			items = append(items, radarItem{
				Title:       title,
				URL:         link,
				Source:      source.Name,
				Summary:     strings.TrimSpace(entry.Description),
				PublishedAt: publishedAt,
			})
		}
	}
	return items
}

func selectTopItems(candidates []radarItem, cfg aiRadarConfig, now time.Time, blocked map[string]bool) []radarItem {
	if len(candidates) == 0 {
		return nil
	}

	var freshCandidates []radarItem
	for _, item := range candidates {
		published := parseTime(item.PublishedAt)
		if !published.IsZero() && now.Sub(published) > 45*24*time.Hour {
			continue
		}
		freshCandidates = append(freshCandidates, item)
	}
	candidates = freshCandidates
	if len(candidates) == 0 {
		return nil
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left := itemScore(candidates[i], now)
		right := itemScore(candidates[j], now)
		if left != right {
			return left > right
		}
		leftTime := parseTime(candidates[i].PublishedAt)
		rightTime := parseTime(candidates[j].PublishedAt)
		if !leftTime.Equal(rightTime) {
			return leftTime.After(rightTime)
		}
		return strings.ToLower(candidates[i].Title) < strings.ToLower(candidates[j].Title)
	})

	selected := pickItems(candidates, cfg.MaxItems, blocked, true)
	if len(selected) < cfg.MaxItems {
		selected = append(selected, pickItems(filterOut(candidates, selected), cfg.MaxItems-len(selected), nil, true)...)
	}
	if len(selected) < cfg.MaxItems {
		selected = append(selected, pickItems(filterOut(candidates, selected), cfg.MaxItems-len(selected), nil, false)...)
	}

	selected = append(selected, fillFallbacks(cfg.FallbackItems, selected, cfg.MaxItems)...)
	if len(selected) > cfg.MaxItems {
		selected = selected[:cfg.MaxItems]
	}
	return selected
}

func recentURLMap(state radarState, window time.Duration, now time.Time) map[string]bool {
	recent := make(map[string]bool, len(state.Recent))
	for _, seen := range pruneState(state, window, now).Recent {
		recent[canonicalURL(seen.URL)] = true
	}
	return recent
}

func pickItems(candidates []radarItem, limit int, blocked map[string]bool, uniqueSource bool) []radarItem {
	var picked []radarItem
	usedSources := map[string]bool{}
	usedURLs := map[string]bool{}
	for _, item := range candidates {
		if len(picked) >= limit {
			break
		}
		url := canonicalURL(item.URL)
		if url == "" || usedURLs[url] {
			continue
		}
		if blocked != nil && blocked[url] {
			continue
		}
		if uniqueSource && usedSources[item.Source] {
			continue
		}
		picked = append(picked, item)
		usedSources[item.Source] = true
		usedURLs[url] = true
	}
	return picked
}

func fillFallbacks(candidates, selected []radarItem, limit int) []radarItem {
	if len(selected) >= limit {
		return nil
	}
	selectedURLs := map[string]bool{}
	for _, item := range selected {
		selectedURLs[canonicalURL(item.URL)] = true
	}
	var extras []radarItem
	for _, item := range candidates {
		if len(selected)+len(extras) >= limit {
			break
		}
		url := canonicalURL(item.URL)
		if url == "" || selectedURLs[url] {
			continue
		}
		extras = append(extras, item)
		selectedURLs[url] = true
	}
	return extras
}

func filterOut(candidates, selected []radarItem) []radarItem {
	selectedURLs := map[string]bool{}
	for _, item := range selected {
		selectedURLs[canonicalURL(item.URL)] = true
	}
	var filtered []radarItem
	for _, item := range candidates {
		if !selectedURLs[canonicalURL(item.URL)] {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func itemScore(item radarItem, now time.Time) int {
	text := strings.ToLower(strings.TrimSpace(item.Title + " " + item.Summary + " " + item.URL))
	score := 0

	published := parseTime(item.PublishedAt)
	if !published.IsZero() {
		age := now.Sub(published)
		switch {
		case age < 24*time.Hour:
			score += 60
		case age < 72*time.Hour:
			score += 45
		case age < 7*24*time.Hour:
			score += 30
		case age < 14*24*time.Hour:
			score += 10
		}
	}

	positive := map[string]int{
		"model":        16,
		"models":       14,
		"open model":   18,
		"open-source":  12,
		"open source":  12,
		"benchmark":    18,
		"eval":         16,
		"evaluation":   16,
		"agent":        12,
		"agents":       12,
		"api":          8,
		"inference":    12,
		"reranker":     12,
		"embedding":    10,
		"embeddings":   10,
		"multimodal":   10,
		"reasoning":    10,
		"coding":       10,
		"release":      8,
		"launch":       8,
		"launches":     8,
		"runtime":      8,
		"gpu":          6,
		"vector":       6,
		"rag":          8,
		"tool":         5,
		"tools":        5,
		"scoring":      8,
		"fine-tuning":  8,
		"fine tuning":  8,
		"llm":          8,
		"gemma":        14,
		"gemini":       12,
		"gpt":          10,
		"mlx":          10,
		"world model":  12,
		"world models": 12,
	}
	negative := map[string]int{
		"academy":          -80,
		"customer success": -30,
		"conference":       -10,
		"event":            -4,
		"podcast":          -12,
		"webinar":          -12,
		"workshop":         -10,
		"recap":            -8,
		"video":            -6,
		"fundamentals":     -60,
		"what is ai":       -60,
		"learn how":        -20,
	}

	for term, weight := range positive {
		if strings.Contains(text, term) {
			score += weight
		}
	}
	for term, weight := range negative {
		if strings.Contains(text, term) {
			score += weight
		}
	}

	switch item.Source {
	case "Hugging Face":
		score += 6
	case "Latent.Space":
		score += 5
	case "Ollama":
		score += 5
	case "DeepMind":
		score += 4
	case "Google AI":
		score += 4
	case "OpenAI":
		score += 3
	}

	return score
}

func renderReadme(profile profileConfig, radar []radarItem) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n\n", profile.Name)
	fmt.Fprintf(&b, "%s\n\n", profile.Hero.Title)
	fmt.Fprintf(&b, "%s\n\n", profile.Hero.Body)
	fmt.Fprintf(&b, "GitHub is the code-side companion to [%s](%s), where the broader portfolio, selected work, and case-study view live.\n\n", profile.PortfolioLabel, profile.PortfolioURL)
	fmt.Fprintf(&b, "[Portfolio](%s) · [LinkedIn](%s) · [Resume](%s)\n\n", profile.PortfolioURL, profile.LinkedInURL, profile.ResumeURL)

	writeBullets(&b, "Now", profile.Now)
	writeProjects(&b, "Active Builds", profile.ActiveBuilds)
	writeProjects(&b, "Open Source Tools", profile.OpenSourceTools)
	writePrivateSystems(&b, "Private Systems I Can Describe Publicly", profile.PrivateSystems)
	writeAIRadar(&b, radar)
	writeCTA(&b, profile.WorkWithMe)
	writeCTA(&b, profile.Contribute)

	fmt.Fprintf(&b, "## Full Portfolio\n\nFor the polished portfolio, selected work, and fuller capability map, head to [%s](%s).\n\n", profile.PortfolioLabel, profile.PortfolioURL)
	fmt.Fprintf(&b, "![Snake animation](https://github.com/Esturban/Esturban/blob/output/github-contribution-grid-snake.svg)\n\n")
	fmt.Fprintf(&b, "<sub>Rendered from structured profile data plus the latest stable AI Radar selection.</sub>\n")

	return b.String()
}

func writeBullets(b *strings.Builder, title string, bullets []string) {
	if len(bullets) == 0 {
		return
	}
	fmt.Fprintf(b, "## %s\n\n", title)
	for _, bullet := range bullets {
		fmt.Fprintf(b, "- %s\n", bullet)
	}
	fmt.Fprintln(b)
}

func writeProjects(b *strings.Builder, title string, projects []projectCard) {
	if len(projects) == 0 {
		return
	}
	fmt.Fprintf(b, "## %s\n\n", title)
	for _, project := range projects {
		fmt.Fprintf(b, "- [%s](%s) — %s\n", project.Name, project.URL, project.Summary)
	}
	fmt.Fprintln(b)
}

func writePrivateSystems(b *strings.Builder, title string, systems []privateCard) {
	if len(systems) == 0 {
		return
	}
	fmt.Fprintf(b, "## %s\n\n", title)
	for _, system := range systems {
		fmt.Fprintf(b, "- **%s** — %s\n", system.Name, system.Summary)
	}
	fmt.Fprintln(b)
}

func writeAIRadar(b *strings.Builder, radar []radarItem) {
	if len(radar) == 0 {
		return
	}
	fmt.Fprintf(b, "## AI Radar\n\n")
	for _, item := range radar {
		fmt.Fprintf(b, "- [%s](%s) — %s · %s\n", item.Title, item.URL, item.Source, publishedLabel(item.PublishedAt))
	}
	fmt.Fprintf(b, "\nBenchmark pulse: I check [LiveBench](https://livebench.ai) daily.\n\n")
}

func writeCTA(b *strings.Builder, block ctaBlock) {
	if block.Title == "" || block.Body == "" {
		return
	}
	fmt.Fprintf(b, "## %s\n\n%s\n\n", block.Title, block.Body)
}

func publishedLabel(raw string) string {
	published := parseTime(raw)
	if published.IsZero() {
		return "reference"
	}
	return published.Format("2 Jan 2006")
}

func pruneState(state radarState, window time.Duration, now time.Time) radarState {
	var pruned []seenItem
	for _, item := range state.Recent {
		seenAt := parseTime(item.SeenAt)
		if seenAt.IsZero() {
			continue
		}
		if now.Sub(seenAt) <= window {
			pruned = append(pruned, seenItem{
				URL:    canonicalURL(item.URL),
				SeenAt: seenAt.UTC().Format(time.RFC3339),
			})
		}
	}
	state.Recent = pruned
	return state
}

func writeIfChanged(path string, content []byte) error {
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, content) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func sameItems(left, right []radarItem) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].Title != right[i].Title || canonicalURL(left[i].URL) != canonicalURL(right[i].URL) || left[i].Source != right[i].Source {
			return false
		}
	}
	return true
}

func cloneItems(items []radarItem) []radarItem {
	cloned := make([]radarItem, len(items))
	copy(cloned, items)
	return cloned
}

func parseTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func canonicalURL(raw string) string {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimSuffix(cleaned, "/")
	return cleaned
}

func failf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
