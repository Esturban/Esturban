package main

import (
	"strings"
	"testing"
)

func TestWriteGitHubStatsDisabled(t *testing.T) {
	var b strings.Builder
	writeGitHubStats(&b, githubStats{})
	if b.Len() != 0 {
		t.Fatalf("expected no output, got %q", b.String())
	}
}

func TestWriteGitHubStatsRendersDashboardRow(t *testing.T) {
	var b strings.Builder
	writeGitHubStats(&b, githubStats{
		Enabled:  true,
		Username: "Esturban",
	})

	out := b.String()
	checks := []string{
		"## GitHub Snapshot",
		"<p align=\"left\">",
		"width=\"49%\"",
		"github-readme-stats.vercel.app/api?",
		"streak-stats.demolab.com/?",
		"hide_border=true",
		"theme=transparent",
		"hide_title=true",
		`alt="GitHub stats for Esturban"`,
		`alt="GitHub streak for Esturban"`,
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got %q", want, out)
		}
	}
}
