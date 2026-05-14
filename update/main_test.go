package main

import (
	"strings"
	"testing"
)

func TestWriteGitHubStatsDisabled(t *testing.T) {
	var b strings.Builder
	writeGitHubStats(&b, githubStats{}, githubSnapshot{})
	if b.Len() != 0 {
		t.Fatalf("expected no output, got %q", b.String())
	}
}

func TestWriteGitHubStatsRendersBadgesAndStreak(t *testing.T) {
	var b strings.Builder
	writeGitHubStats(&b, githubStats{
		Enabled:  true,
		Username: "Esturban",
	}, githubSnapshot{
		PublicRepos:      43,
		PublicGists:      12,
		Followers:        16,
		Following:        49,
		TotalStars:       8,
		FeaturedStars:    3,
		HasUserStats:     true,
		HasTotalStars:    true,
		HasFeaturedStars: true,
	})

	out := b.String()
	checks := []string{
		"## GitHub Snapshot",
		"img.shields.io/badge/Public%20Repos-43-0969da",
		"img.shields.io/badge/Followers-16-0969da",
		"img.shields.io/badge/Following-49-0969da",
		"img.shields.io/badge/Public%20Gists-12-0969da",
		"img.shields.io/badge/Total%20Stars-8-0969da",
		"img.shields.io/badge/Featured%20Stars-3-0969da",
		"streak-stats.demolab.com/?",
		"theme=transparent",
		"width=\"72%\"",
		`alt="GitHub streak for Esturban"`,
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got %q", want, out)
		}
	}
	if strings.Contains(out, "github-readme-stats.vercel.app/api?") {
		t.Fatalf("expected output to avoid paused stats service, got %q", out)
	}
}
