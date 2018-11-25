package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strconv"

	"github.com/gocolly/colly"
)

var baseSearchURL = "https://factba.se/json/json-transcript.php?q=&f=&dt=&p="
var baseTranscriptURL = "https://factba.se/transcript/"

type result struct {
	Slug string `json:"slug"`
	Date string `json:"date"`
}

type results struct {
	Data []*result `json:"data"`
}

type transcript struct {
	Speaker string
	Text    string
}

func main() {
	c := colly.NewCollector(
		colly.AllowedDomains("factba.se"),
	)

	d := c.Clone()

	d.OnHTML("body", func(_ context.Context, e *colly.HTMLElement) {
		t := make([]transcript, 0)
		e.ForEach(".topic-media-row", func(_ int, el *colly.HTMLElement) {
			t = append(t, transcript{
				Speaker: el.ChildText(".speaker-label"),
				Text:    el.ChildText(".transcript-text-block"),
			})
		})
		jsonData, err := json.MarshalIndent(t, "", "  ")
		if err != nil {
			return
		}
		dctx := colly.ContextDataContext(e.Request.Ctx)
		ioutil.WriteFile(colly.SanitizeFileName(dctx.Get("date")+"_"+dctx.Get("slug"))+".json", jsonData, 0644)
	})

	stop := false
	c.OnResponse(func(_ context.Context, r *colly.Response) {
		rs := &results{}
		err := json.Unmarshal(r.Body, rs)
		if err != nil || len(rs.Data) == 0 {
			stop = true
			return
		}
		for _, res := range rs.Data {
			u := baseTranscriptURL + res.Slug
			ctx, dctx := colly.WithDataContext(context.Background())
			dctx.Put("date", res.Date)
			dctx.Put("slug", res.Slug)
			d.Request(ctx, "GET", u, nil, nil)
		}
	})

	for i := 1; i < 1000; i++ {
		if stop {
			break
		}
		if err := c.Visit(baseSearchURL + strconv.Itoa(i)); err != nil {
			fmt.Println("Error:", err)
			break
		}
	}
}
