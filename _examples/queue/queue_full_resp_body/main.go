package main

import (
	"encoding/csv"
	"fmt"
	"github.com/gocolly/colly"
	"github.com/gocolly/colly/queue"
	"log"
	"os"
)

func main() {
	var result colly.Response

	urls := ReadCsvFile("testcsv.csv")
	for _, url := range urls {

		c := colly.NewCollector()

		q, _ := queue.New(
			2,
			&queue.InMemoryQueueStorage{MaxSize: 10000},
		)

		err := q.AddURL(url[0])
		if err != nil {
			log.Println(err)
			os.Exit(1)
		}

		c.OnHTML("body", func(e *colly.HTMLElement) {
			result = colly.Response{Body: e.Response.Body}
			strResp :=  string(result.Body)

			fileName := url[0][8:]+".html"
			f, err := os.Create(fileName)
			if err != nil {
				panic(err)
			}
			defer func() {
				if err := f.Close(); err != nil {
					panic(err)
				}
			}()

			f.WriteString(strResp)
		})

		c.OnRequest(func(r *colly.Request) {
			fmt.Println("visiting-------------------", r.URL)
		})

		c.OnError(func(r *colly.Response, err error) {
			fmt.Println("Request URL:", r.Request.URL, "It failed to response:", r, "\nError:", err)
		})

		q.Run(c)
	}
}

func ReadCsvFile(filePath string) [][]string {
	file, err := os.Open(filePath)

	if err != nil {
		fmt.Println(err)
	}

	defer file.Close()

	reader := csv.NewReader(file)

	data, err := reader.ReadAll()

	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
	return data
}
