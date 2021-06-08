# Colly

Lightning Fast and Elegant Scraping Framework for Gophers

Colly provides a clean interface to write any kind of crawler/scraper/spider.

With Colly you can easily extract structured data from websites, which can be used for a wide range of applications, like data mining, data processing or archiving.

[![GoDoc](https://godoc.org/github.com/gocolly/colly?status.svg)](https://pkg.go.dev/github.com/gocolly/colly/v2)
[![Backers on Open Collective](https://opencollective.com/colly/backers/badge.svg)](#backers) [![Sponsors on Open Collective](https://opencollective.com/colly/sponsors/badge.svg)](#sponsors) [![build status](https://img.shields.io/travis/gocolly/colly/master.svg?style=flat-square)](https://travis-ci.org/gocolly/colly)
[![report card](https://img.shields.io/badge/report%20card-a%2B-ff3333.svg?style=flat-square)](http://goreportcard.com/report/gocolly/colly)
[![view examples](https://img.shields.io/badge/learn%20by-examples-0077b3.svg?style=flat-square)](https://github.com/gocolly/colly/tree/master/_examples)
[![Code Coverage](https://img.shields.io/codecov/c/github/gocolly/colly/master.svg)](https://codecov.io/github/gocolly/colly?branch=master)
[![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Fgocolly%2Fcolly.svg?type=shield)](https://app.fossa.io/projects/git%2Bgithub.com%2Fgocolly%2Fcolly?ref=badge_shield)
[![Twitter URL](https://img.shields.io/badge/twitter-follow-green.svg)](https://twitter.com/gocolly)

## Features

-   Clean API
-   Fast (>1k request/sec on a single core)
-   Manages request delays and maximum concurrency per domain
-   Automatic cookie and session handling
-   Sync/async/parallel scraping
-   Caching
-   Automatic encoding of non-unicode responses
-   Robots.txt support
-   Distributed scraping
-   Configuration via environment variables
-   Extensions

## Example

```go
func main() {
	c := colly.NewCollector()

	// Find and visit all links
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		e.Request.Visit(e.Attr("href"))
	})

	c.OnRequest(func(r *colly.Request) {
		fmt.Println("Visiting", r.URL)
	})

	c.Visit("http://go-colly.org/")
}
```

See [examples folder](https://github.com/gocolly/colly/tree/master/_examples) for more detailed examples.

## Installation

Add colly to your `go.mod` file:

```
module github.com/x/y

go 1.14

require (
        github.com/gocolly/colly/v2 v2.1.0
)
```

## Bugs

Bugs or suggestions? Visit the [issue tracker](https://github.com/gocolly/colly/issues) or join `#colly` on freenode

## Other Projects Using Colly

Below is a list of public, open source projects that use Colly:

-   [greenpeace/check-my-pages](https://github.com/greenpeace/check-my-pages) Scraping script to test the Spanish Greenpeace web archive.
-   [altsab/gowap](https://github.com/altsab/gowap) Wappalyzer implementation in Go.
-   [jesuiscamille/goquotes](https://github.com/jesuiscamille/goquotes) A quotes scrapper, making your day a little better!
-   [jivesearch/jivesearch](https://github.com/jivesearch/jivesearch) A search engine that doesn't track you.
-   [Leagify/colly-draft-prospects](https://github.com/Leagify/colly-draft-prospects) A scraper for future NFL Draft prospects.
-   [lucasepe/go-ps4](https://github.com/lucasepe/go-ps4) Search playstation store for your favorite PS4 games using the command line.
-   [yringler/inside-chassidus-scraper](https://github.com/yringler/inside-chassidus-scraper) Scrapes Rabbi Paltiel's web site for lesson metadata.
-   [gamedb/gamedb](https://github.com/gamedb/gamedb) A database of Steam games.
-   [lawzava/scrape](https://github.com/lawzava/scrape) CLI for email scraping from any website.
-   [eureka101v/WeiboSpiderGo](https://github.com/eureka101v/WeiboSpiderGo) A sina weibo(chinese twitter) scrapper
-   [Go-phie/gophie](https://github.com/Go-phie/gophie) Search, Download and Stream movies from your terminal
-   [imthaghost/goclone](https://github.com/imthaghost/goclone) Clone websites to your computer within seconds.
-   [superiss/spidy](https://github.com/superiss/spidy) Crawl the web and collect expired domains.
-   [docker-slim/docker-slim](https://github.com/docker-slim/docker-slim) Optimize your Docker containers to make them smaller and better.
-   [seversky/gachifinder](https://github.com/seversky/gachifinder) an agent for asynchronous scraping, parsing and writing to some storages(elasticsearch for now)
-   [eval-exec/goodreads](https://github.com/eval-exec/goodreads) crawl all tags and all pages of quotes from goodreads.

If you are using Colly in a project please send a pull request to add it to the list.

## Contributors

This project exists thanks to all the people who contribute. [[Contribute]](CONTRIBUTING.md).
<a href="https://github.com/gocolly/colly/graphs/contributors"><img src="https://opencollective.com/colly/contributors.svg?width=890" /></a>

## Backers

Thank you to all our backers! 🙏 [[Become a backer](https://opencollective.com/colly#backer)]

<a href="https://opencollective.com/colly#backers" target="_blank"><img src="https://opencollective.com/colly/backers.svg?width=890"></a>

## Sponsors

Support this project by becoming a sponsor. Your logo will show up here with a link to your website. [[Become a sponsor](https://opencollective.com/colly#sponsor)]

<a href="https://opencollective.com/colly/sponsor/0/website" target="_blank"><img src="https://opencollective.com/colly/sponsor/0/avatar.svg"></a>
<a href="https://opencollective.com/colly/sponsor/1/website" target="_blank"><img src="https://opencollective.com/colly/sponsor/1/avatar.svg"></a>
<a href="https://opencollective.com/colly/sponsor/2/website" target="_blank"><img src="https://opencollective.com/colly/sponsor/2/avatar.svg"></a>
<a href="https://opencollective.com/colly/sponsor/3/website" target="_blank"><img src="https://opencollective.com/colly/sponsor/3/avatar.svg"></a>
<a href="https://opencollective.com/colly/sponsor/4/website" target="_blank"><img src="https://opencollective.com/colly/sponsor/4/avatar.svg"></a>
<a href="https://opencollective.com/colly/sponsor/5/website" target="_blank"><img src="https://opencollective.com/colly/sponsor/5/avatar.svg"></a>
<a href="https://opencollective.com/colly/sponsor/6/website" target="_blank"><img src="https://opencollective.com/colly/sponsor/6/avatar.svg"></a>
<a href="https://opencollective.com/colly/sponsor/7/website" target="_blank"><img src="https://opencollective.com/colly/sponsor/7/avatar.svg"></a>
<a href="https://opencollective.com/colly/sponsor/8/website" target="_blank"><img src="https://opencollective.com/colly/sponsor/8/avatar.svg"></a>
<a href="https://opencollective.com/colly/sponsor/9/website" target="_blank"><img src="https://opencollective.com/colly/sponsor/9/avatar.svg"></a>

## License

[![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Fgocolly%2Fcolly.svg?type=large)](https://app.fossa.io/projects/git%2Bgithub.com%2Fgocolly%2Fcolly?ref=badge_large)
