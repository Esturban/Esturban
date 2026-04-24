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
	TechStack       []string      `json:"techStack"`
	ActiveBuilds    []projectCard `json:"activeBuilds"`
	OpenSourceTools []projectCard `json:"openSourceTools"`
	PrivateSystems  []privateCard `json:"privateSystems"`
	WorkWithMe      ctaBlock      `json:"workWithMe"`
	GitHubStats     githubStats   `json:"githubStats"`
	Meta            metaConfig    `json:"meta"`
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

type githubStats struct {
	Enabled  bool   `json:"enabled"`
	Username string `json:"username"`
}

type metaConfig struct {
	Enabled bool `json:"enabled"`
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

type githubRepoResp struct {
	StargazersCount int `json:"stargazers_count"`
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

	stars := fetchAllStars(profile)
	readme := renderReadme(profile, selected, stars)

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
	selected := selectTopItems(candidates, cfg, now, nil)
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

	// Always update lastItems so the README reflects the current best selection.
	// writeIfChanged handles idempotency — no need to short-circuit here.
	nextState.LastItems = cloneItems(selected)

	// Only log URLs not already present in recent to prevent per-cron bloat.
	alreadySeen := make(map[string]bool, len(nextState.Recent))
	for _, s := range nextState.Recent {
		alreadySeen[s.URL] = true
	}
	for _, item := range selected {
		if item.URL == "" {
			continue
		}
		u := canonicalURL(item.URL)
		if !alreadySeen[u] {
			nextState.Recent = append(nextState.Recent, seenItem{
				URL:    u,
				SeenAt: now.Format(time.RFC3339),
			})
			alreadySeen[u] = true
		}
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
		if !published.IsZero() && now.Sub(published) > 21*24*time.Hour {
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

// fetchStarCount hits the public GitHub API (unauthenticated) and returns the
// star count for the given owner/repo. Returns -1 on any error so callers can
// skip the star display gracefully.
func fetchStarCount(owner, repo string) int {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)
	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return -1
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "readme-generator")
	resp, err := client.Do(req)
	if err != nil {
		return -1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return -1
	}
	var gh githubRepoResp
	if err := json.NewDecoder(resp.Body).Decode(&gh); err != nil {
		return -1
	}
	return gh.StargazersCount
}

// fetchAllStars returns a map of repo name → star count for all public repos
// listed in the profile. Failures are silently ignored (star count omitted).
func fetchAllStars(profile profileConfig) map[string]int {
	owner := githubOwner(profile.GitHubURL)
	if owner == "" {
		return nil
	}
	stars := make(map[string]int)
	repos := make([]projectCard, 0, len(profile.ActiveBuilds)+len(profile.OpenSourceTools))
	repos = append(repos, profile.ActiveBuilds...)
	repos = append(repos, profile.OpenSourceTools...)
	for _, p := range repos {
		n := fetchStarCount(owner, p.Name)
		if n >= 0 {
			stars[p.Name] = n
		}
	}
	return stars
}

// githubOwner extracts the username from a GitHub profile URL.
func githubOwner(githubURL string) string {
	trimmed := strings.TrimSuffix(strings.TrimSpace(githubURL), "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func renderReadme(profile profileConfig, radar []radarItem, stars map[string]int) string {
	var b strings.Builder

	// Header
	fmt.Fprintf(&b, "# %s\n\n", profile.Name)
	fmt.Fprintf(&b, "%s\n\n", profile.Hero.Title)
	// Visitor counter — live badge, no config needed
	owner := githubOwner(profile.GitHubURL)
	fmt.Fprintf(&b, "![Profile Views](https://komarev.com/ghpvc/?username=%s&color=blue&style=flat-square&label=profile+views)\n\n", owner)
	fmt.Fprintf(&b, "[Portfolio](%s) · [LinkedIn](%s) · [Resume](%s)\n\n", profile.PortfolioURL, profile.LinkedInURL, profile.ResumeURL)

	// CTA first
	writeCTA(&b, profile.WorkWithMe)

	// Now — tight bullets
	writeBullets(&b, "Now", profile.Now)

	// Tech stack badges
	writeTechStack(&b, profile.TechStack)

	// Private work — compact pointer to portfolio
	writePrivateSystems(&b, profile.PrivateSystems, profile.PortfolioURL, profile.PortfolioLabel)

	// Live signals: stats + streak side by side, then AI Radar
	writeGitHubStats(&b, profile.GitHubStats)
	writeAIRadar(&b, radar)

	// Collapsed meta footer
	writeMeta(&b, profile)

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

func writeProjects(b *strings.Builder, title string, projects []projectCard, stars map[string]int) {
	if len(projects) == 0 {
		return
	}
	fmt.Fprintf(b, "## %s\n\n", title)
	for _, project := range projects {
		starStr := ""
		if n, ok := stars[project.Name]; ok && n >= 0 {
			starStr = fmt.Sprintf(" ★%d", n)
		}
		fmt.Fprintf(b, "- [%s](%s)%s — %s\n", project.Name, project.URL, starStr, project.Summary)
	}
	fmt.Fprintln(b)
}

func writePrivateSystems(b *strings.Builder, systems []privateCard, portfolioURL, portfolioLabel string) {
	if len(systems) == 0 {
		return
	}
	names := make([]string, len(systems))
	for i, s := range systems {
		names[i] = s.Name
	}
	fmt.Fprintf(b, "**Private work:** %s — [%s](%s)\n\n", strings.Join(names, " · "), portfolioLabel, portfolioURL)
}

// techBadgeURL maps a tech label to its shields.io badge image URL.
var techBadgeURL = map[string]string{
	"Go":             "https://img.shields.io/badge/Go-00ADD8?style=flat-square&logo=go&logoColor=white",
	"Python":         "https://img.shields.io/badge/Python-3776AB?style=flat-square&logo=python&logoColor=white",
	"PostgreSQL":     "https://img.shields.io/badge/PostgreSQL-316192?style=flat-square&logo=postgresql&logoColor=white",
	"Docker":         "https://img.shields.io/badge/Docker-2496ED?style=flat-square&logo=docker&logoColor=white",
	"Bash":           "https://img.shields.io/badge/Bash-4EAA25?style=flat-square&logo=gnubash&logoColor=white",
	"GitHub Actions": "https://img.shields.io/badge/GitHub_Actions-2088FF?style=flat-square&logo=github-actions&logoColor=white",
	"SQL":            "https://img.shields.io/badge/SQL-CC2927?style=flat-square&logo=amazondynamodb&logoColor=white",
	"Rust":           "https://img.shields.io/badge/Rust-000000?style=flat-square&logo=rust&logoColor=white",
	"TypeScript":     "https://img.shields.io/badge/TypeScript-007ACC?style=flat-square&logo=typescript&logoColor=white",
	"JavaScript":     "https://img.shields.io/badge/JavaScript-F7DF1E?style=flat-square&logo=javascript&logoColor=black",
	"Terraform":      "https://img.shields.io/badge/Terraform-623CE4?style=flat-square&logo=terraform&logoColor=white",
	"AWS":            "https://img.shields.io/badge/AWS-FF9900?style=flat-square&logo=amazonaws&logoColor=white",
	"GCP":            "https://img.shields.io/badge/GCP-4285F4?style=flat-square&logo=googlecloud&logoColor=white",
	"Redis":          "https://img.shields.io/badge/Redis-DC382D?style=flat-square&logo=redis&logoColor=white",
	"Kubernetes":     "https://img.shields.io/badge/Kubernetes-326CE5?style=flat-square&logo=kubernetes&logoColor=white",
}

func writeTechStack(b *strings.Builder, stack []string) {
	if len(stack) == 0 {
		return
	}
	var badges []string
	for _, tech := range stack {
		if url, ok := techBadgeURL[tech]; ok {
			badges = append(badges, fmt.Sprintf("![%s](%s)", tech, url))
		}
	}
	if len(badges) == 0 {
		return
	}
	fmt.Fprintf(b, "%s\n\n", strings.Join(badges, " "))
}

func writeGitHubStats(b *strings.Builder, cfg githubStats) {
	if !cfg.Enabled || cfg.Username == "" {
		return
	}
	statsURL := fmt.Sprintf(
		"https://github-readme-stats.vercel.app/api?username=%s&show_icons=true&hide_border=true&count_private=true",
		cfg.Username,
	)
	streakURL := fmt.Sprintf(
		"https://streak-stats.demolab.com?user=%s&hide_border=true",
		cfg.Username,
	)
	fmt.Fprintf(b, "![GitHub Stats](%s)\n", statsURL)
	fmt.Fprintf(b, "![GitHub Streak](%s)\n\n", streakURL)
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

func writeMeta(b *strings.Builder, profile profileConfig) {
	if !profile.Meta.Enabled {
		return
	}
	owner := githubOwner(profile.GitHubURL)
	sourceURL := fmt.Sprintf("https://github.com/%s/%s/tree/main/update", owner, owner)
	fmt.Fprintf(b,
		"<sub>Live artifact — Go + GitHub Actions rewrites this every 6h, scoring 6 AI feeds by recency and relevance. [Source →](%s) · [LiveBench](https://livebench.ai)</sub>\n",
		sourceURL,
	)
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
