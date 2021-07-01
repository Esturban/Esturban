package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"time"
	"github.com/mmcdole/gofeed"
)

func makeReadme(filename string) error {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL("https://victoria.dev/index.xml")
	if err != nil {
		log.Fatalf("error getting feed: %v", err)
	}
	// Get the freshest item
	//blogItem := feed.Items[0]

	wc, err := fp.ParseURL("https://victoria.dev/wc/index.xml")
	if err != nil {
		log.Fatalf("error getting feed: %v", err)
	}
	// Add this much magic
	//wcItem := wc.Items[0]
    fmt.Println(feed.Title)
    fmt.Println(wc.Title)
	date := time.Now().Format("2 Jan 2006")

	// Whisk together static and dynamic content until stiff peaks form
	nav := "# <video autoplay=\"\" height=\"32\" data-vscid=\"wxo1z48g0\"><source src=\"https://www.estebanvalencia.com/me/tux.webm\" type=\"video/webm\"></video> Esteban Valencia"
	header := "## Engineer / Developer / Product Development / Traveler"
	ghs := "<p align=\"center\">[![Esturban's GitHub stats](https://github-readme-stats.vercel.app/api?username=Esturban&count_private=true&show_icons=true&theme=algolia)](https://github.com/anuraghazra/github-readme-stats)</p>"
    lk := "<a href=\"https://www.linkedin.com/in/valest/\" target=\"_blank\"><img src=\"https://img.shields.io/badge/linkedin-valest-blue?style=for-the-badge&logo=linkedin\" /></a>"
    social := lk
	blog := "<p align=\"center\"><img src=\"https://media.giphy.com/media/BTuF8rhHsnosLQPpGB/giphy.gif\" alt = \"ZTFO\"/></p>"
	updated := "<sub>Last updated by magic on " + date + ".</sub>"
	data := fmt.Sprintf("%s\n\n%s\n\n%s\n\n%s\n\n%s\n\n%s\n", nav, header, ghs, social, blog, updated)

	// Prepare file with a light coating of os
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Bake at n bytes per second until golden brown
	_, err = io.WriteString(file, data)
	if err != nil {
		return err
	}
	return file.Sync()
}

func main() {

	makeReadme("../README.md")

}