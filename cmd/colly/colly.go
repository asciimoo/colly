// Copyright 2018 Adam Tauber
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/jawher/mow.cli"
)

var scraperHeadTemplate = `package main

import (
	"log"

	"github.com/gocolly/colly/v2"
)

func main() {
	c := colly.NewCollector()
`

var scraperEndTemplate = `
	c.Visit("https://yourdomain.com/")
}
`

var htmlCallbackTemplate = `
	c.OnHTML("element-selector", func(e *colly.HTMLElement) {
		log.Println(e.Text)
	})
`

var requestCallbackTemplate = `
	c.OnRequest(func(r *colly.Request) {
		log.Println("Visiting", r.URL)
	})
`

var responseCallbackTemplate = `
	c.OnResponse(func(r *colly.Response) {
		log.Println("Visited", r.Request.URL, r.StatusCode)
	})
`

var errorCallbackTemplate = `
	c.OnError(func(r *colly.Response, err error) {
		log.Printf("Error on %s: %s", r.Request.URL, err)
	})
`

func main() {
	app := cli.App("colly", "Scraping Framework for Gophers")

	app.Command("new", "Create new scraper", func(cmd *cli.Cmd) {
		var (
			callbacks = cmd.StringOpt("callbacks", "", "Add callbacks to the template. (E.g. '--callbacks=html,response,error')")
			hosts     = cmd.StringOpt("hosts", "", "Specify scraper's allowed hosts. (e.g. '--hosts=xy.com,abcd.com')")
			path      = cmd.StringArg("PATH", "", "Path of the new scraper")
		)

		cmd.Spec = "[--callbacks] [--hosts] [PATH]"

		cmd.Action = func() {
			scraper := bytes.NewBufferString(scraperHeadTemplate)
			outfile := os.Stdout
			if *path != "" {
				var err error
				outfile, err = os.Create(*path)
				if err != nil {
					log.Fatal(err)
				}
				defer outfile.Close()
			}
			if *hosts != "" {
				scraper.WriteString("\n	c.AllowedDomains = []string{")
				for i, h := range strings.Split(*hosts, ",") {
					if i > 0 {
						scraper.WriteString(", ")
					}
					scraper.WriteString(fmt.Sprintf("%q", h))
				}
				scraper.WriteString("}\n")
			}
			if len(*callbacks) > 0 {
				for _, c := range strings.Split(*callbacks, ",") {
					switch c {
					case "html":
						scraper.WriteString(htmlCallbackTemplate)
					case "request":
						scraper.WriteString(requestCallbackTemplate)
					case "response":
						scraper.WriteString(responseCallbackTemplate)
					case "error":
						scraper.WriteString(errorCallbackTemplate)
					}
				}
			}
			scraper.WriteString(scraperEndTemplate)
			outfile.Write(scraper.Bytes())
		}
	})

	app.Run(os.Args)
}
