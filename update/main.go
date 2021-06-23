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

	date := time.Now().Format("2 Jan 2006")

	// Whisk together static and dynamic content until stiff peaks form
	hello := "Hello there 👋"
	blog := "<p align=\"center\"><img src=\"https://media.giphy.com/media/BTuF8rhHsnosLQPpGB/giphy.gif\" alt = \"ZTFO\"/></p>"
	updated := "<sub>Last updated by magic on " + date + ".</sub>"
	data := fmt.Sprintf("%s\n\n%s\n\n%s\n", hello, blog, updated)

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