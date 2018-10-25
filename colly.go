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

// Package colly implements a HTTP scraping framework
package colly

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/html"
	"google.golang.org/appengine/urlfetch"

	"github.com/PuerkitoBio/goquery"
	"github.com/antchfx/htmlquery"
	"github.com/antchfx/xmlquery"
	"github.com/kennygrant/sanitize"
	"github.com/temoto/robotstxt"

	"github.com/gocolly/colly/debug"
	"github.com/gocolly/colly/storage"
)

// Collector provides the scraper instance for a scraping job
type Collector struct {
	// UserAgent is the User-Agent string used by HTTP requests
	UserAgent string
	// MaxDepth limits the recursion depth of visited URLs.
	// Set it to 0 for infinite recursion (default).
	MaxDepth int
	// AllowedDomains is a domain whitelist.
	// Leave it blank to allow any domains to be visited
	AllowedDomains []string
	// DisallowedDomains is a domain blacklist.
	DisallowedDomains []string
	// DisallowedURLFilters is a list of regular expressions which restricts
	// visiting URLs. If any of the rules matches to a URL the
	// request will be stopped. DisallowedURLFilters will
	// be evaluated before URLFilters
	// Leave it blank to allow any URLs to be visited
	DisallowedURLFilters []*regexp.Regexp
	// URLFilters is a list of regular expressions which restricts
	// visiting URLs. If any of the rules matches to a URL the
	// request won't be stopped. DisallowedURLFilters will
	// be evaluated before URLFilters

	// Leave it blank to allow any URLs to be visited
	URLFilters []*regexp.Regexp

	// AllowURLRevisit allows multiple downloads of the same URL
	AllowURLRevisit bool
	// MaxBodySize is the limit of the retrieved response body in bytes.
	// 0 means unlimited.
	// The default value for MaxBodySize is 10MB (10 * 1024 * 1024 bytes).
	MaxBodySize int
	// CacheDir specifies a location where GET requests are cached as files.
	// When it's not defined, caching is disabled.
	CacheDir string
	// IgnoreRobotsTxt allows the Collector to ignore any restrictions set by
	// the target host's robots.txt file.  See http://www.robotstxt.org/ for more
	// information.
	IgnoreRobotsTxt bool
	// Async turns on asynchronous network communication. Use Collector.Wait() to
	// be sure all requests have been finished.
	Async bool
	// ParseHTTPErrorResponse allows parsing HTTP responses with non 2xx status codes.
	// By default, Colly parses only successful HTTP responses. Set ParseHTTPErrorResponse
	// to true to enable it.
	ParseHTTPErrorResponse bool
	// ID is the unique identifier of a collector
	ID uint32
	// DetectCharset can enable character encoding detection for non-utf8 response bodies
	// without explicit charset declaration. This feature uses https://github.com/saintfish/chardet
	DetectCharset bool
	// RedirectHandler allows control on how a redirect will be managed
	RedirectHandler   func(req *http.Request, via []*http.Request) error
	store             storage.Storage
	debugger          debug.Debugger
	robotsMap         map[string]*robotstxt.RobotsData
	htmlCallbacks     []*htmlCallbackContainer
	xmlCallbacks      []*xmlCallbackContainer
	requestCallbacks  []RequestCallback
	responseCallbacks []ResponseCallback
	errorCallbacks    []ErrorCallback
	scrapedCallbacks  []ScrapedCallback
	requestCount      uint32
	responseCount     uint32
	backend           *httpBackend
	wg                *sync.WaitGroup
	lock              *sync.RWMutex
}

// RequestCallback is a type alias for OnRequest callback functions
type RequestCallback func(context.Context, *Request)

// ResponseCallback is a type alias for OnResponse callback functions
type ResponseCallback func(context.Context, *Response)

// HTMLCallback is a type alias for OnHTML callback functions
type HTMLCallback func(context.Context, *HTMLElement)

// XMLCallback is a type alias for OnXML callback functions
type XMLCallback func(context.Context, *XMLElement)

// ErrorCallback is a type alias for OnError callback functions
type ErrorCallback func(context.Context, *Response, error)

// ScrapedCallback is a type alias for OnScraped callback functions
type ScrapedCallback func(context.Context, *Response)

// ProxyFunc is a type alias for proxy setter functions.
type ProxyFunc func(*http.Request) (*url.URL, error)

type htmlCallbackContainer struct {
	Selector string
	Function HTMLCallback
}

type xmlCallbackContainer struct {
	Query    string
	Function XMLCallback
}

type cookieJarSerializer struct {
	store storage.Storage
	lock  *sync.RWMutex
}

var collectorCounter uint32

// The key type is unexported to prevent collisions with context keys defined in
// other packages.
type key int

// ProxyURLKey is the context key for the request proxy address.
const ProxyURLKey key = iota

var (
	// ErrForbiddenDomain is the error thrown if visiting
	// a domain which is not allowed in AllowedDomains
	ErrForbiddenDomain = errors.New("Forbidden domain")
	// ErrMissingURL is the error type for missing URL errors
	ErrMissingURL = errors.New("Missing URL")
	// ErrMaxDepth is the error type for exceeding max depth
	ErrMaxDepth = errors.New("Max depth limit reached")
	// ErrForbiddenURL is the error thrown if visiting
	// a URL which is not allowed by URLFilters
	ErrForbiddenURL = errors.New("ForbiddenURL")

	// ErrNoURLFiltersMatch is the error thrown if visiting
	// a URL which is not allowed by URLFilters
	ErrNoURLFiltersMatch = errors.New("No URLFilters match")
	// ErrAlreadyVisited is the error type for already visited URLs
	ErrAlreadyVisited = errors.New("URL already visited")
	// ErrRobotsTxtBlocked is the error type for robots.txt errors
	ErrRobotsTxtBlocked = errors.New("URL blocked by robots.txt")
	// ErrNoCookieJar is the error type for missing cookie jar
	ErrNoCookieJar = errors.New("Cookie jar is not available")
	// ErrNoPattern is the error type for LimitRules without patterns
	ErrNoPattern = errors.New("No pattern defined in LimitRule")
)

var envMap = map[string]func(*Collector, string){
	"ALLOWED_DOMAINS": func(c *Collector, val string) {
		c.AllowedDomains = strings.Split(val, ",")
	},
	"CACHE_DIR": func(c *Collector, val string) {
		c.CacheDir = val
	},
	"DETECT_CHARSET": func(c *Collector, val string) {
		c.DetectCharset = isYesString(val)
	},
	"DISABLE_COOKIES": func(c *Collector, _ string) {
		c.backend.Client.Jar = nil
	},
	"DISALLOWED_DOMAINS": func(c *Collector, val string) {
		c.DisallowedDomains = strings.Split(val, ",")
	},
	"IGNORE_ROBOTSTXT": func(c *Collector, val string) {
		c.IgnoreRobotsTxt = isYesString(val)
	},
	"FOLLOW_REDIRECTS": func(c *Collector, val string) {
		if !isYesString(val) {
			c.RedirectHandler = func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			}
		}
	},
	"MAX_BODY_SIZE": func(c *Collector, val string) {
		size, err := strconv.Atoi(val)
		if err == nil {
			c.MaxBodySize = size
		}
	},
	"MAX_DEPTH": func(c *Collector, val string) {
		maxDepth, err := strconv.Atoi(val)
		if err != nil {
			c.MaxDepth = maxDepth
		}
	},
	"PARSE_HTTP_ERROR_RESPONSE": func(c *Collector, val string) {
		c.ParseHTTPErrorResponse = isYesString(val)
	},
	"USER_AGENT": func(c *Collector, val string) {
		c.UserAgent = val
	},
}

// NewCollector creates a new Collector instance with default configuration
func NewCollector(options ...func(*Collector)) *Collector {
	c := &Collector{}
	c.Init()

	for _, f := range options {
		f(c)
	}

	c.parseSettingsFromEnv()

	return c
}

// UserAgent sets the user agent used by the Collector.
func UserAgent(ua string) func(*Collector) {
	return func(c *Collector) {
		c.UserAgent = ua
	}
}

// MaxDepth limits the recursion depth of visited URLs.
func MaxDepth(depth int) func(*Collector) {
	return func(c *Collector) {
		c.MaxDepth = depth
	}
}

// AllowedDomains sets the domain whitelist used by the Collector.
func AllowedDomains(domains ...string) func(*Collector) {
	return func(c *Collector) {
		c.AllowedDomains = domains
	}
}

// ParseHTTPErrorResponse allows parsing responses with HTTP errors
func ParseHTTPErrorResponse() func(*Collector) {
	return func(c *Collector) {
		c.ParseHTTPErrorResponse = true
	}
}

// DisallowedDomains sets the domain blacklist used by the Collector.
func DisallowedDomains(domains ...string) func(*Collector) {
	return func(c *Collector) {
		c.DisallowedDomains = domains
	}
}

// DisallowedURLFilters sets the list of regular expressions which restricts
// visiting URLs. If any of the rules matches to a URL the request will be stopped.
func DisallowedURLFilters(filters ...*regexp.Regexp) func(*Collector) {
	return func(c *Collector) {
		c.DisallowedURLFilters = filters
	}
}

// URLFilters sets the list of regular expressions which restricts
// visiting URLs. If any of the rules matches to a URL the request won't be stopped.
func URLFilters(filters ...*regexp.Regexp) func(*Collector) {
	return func(c *Collector) {
		c.URLFilters = filters
	}
}

// AllowURLRevisit instructs the Collector to allow multiple downloads of the same URL
func AllowURLRevisit() func(*Collector) {
	return func(c *Collector) {
		c.AllowURLRevisit = true
	}
}

// MaxBodySize sets the limit of the retrieved response body in bytes.
func MaxBodySize(sizeInBytes int) func(*Collector) {
	return func(c *Collector) {
		c.MaxBodySize = sizeInBytes
	}
}

// CacheDir specifies the location where GET requests are cached as files.
func CacheDir(path string) func(*Collector) {
	return func(c *Collector) {
		c.CacheDir = path
	}
}

// IgnoreRobotsTxt instructs the Collector to ignore any restrictions
// set by the target host's robots.txt file.
func IgnoreRobotsTxt() func(*Collector) {
	return func(c *Collector) {
		c.IgnoreRobotsTxt = true
	}
}

// ID sets the unique identifier of the Collector.
func ID(id uint32) func(*Collector) {
	return func(c *Collector) {
		c.ID = id
	}
}

// Async turns on asynchronous network requests.
func Async(a bool) func(*Collector) {
	return func(c *Collector) {
		c.Async = a
	}
}

// DetectCharset enables character encoding detection for non-utf8 response bodies
// without explicit charset declaration. This feature uses https://github.com/saintfish/chardet
func DetectCharset() func(*Collector) {
	return func(c *Collector) {
		c.DetectCharset = true
	}
}

// Debugger sets the debugger used by the Collector.
func Debugger(d debug.Debugger) func(*Collector) {
	return func(c *Collector) {
		d.Init()
		c.debugger = d
	}
}

// Init initializes the Collector's private variables and sets default
// configuration for the Collector
func (c *Collector) Init() {
	c.UserAgent = "colly - https://github.com/gocolly/colly"
	c.MaxDepth = 0
	c.store = &storage.InMemoryStorage{}
	c.store.Init()
	c.MaxBodySize = 10 * 1024 * 1024
	c.backend = &httpBackend{}
	jar, _ := cookiejar.New(nil)
	c.backend.Init(jar)
	c.backend.Client.CheckRedirect = c.checkRedirectFunc()
	c.wg = &sync.WaitGroup{}
	c.lock = &sync.RWMutex{}
	c.robotsMap = make(map[string]*robotstxt.RobotsData)
	c.IgnoreRobotsTxt = true
	c.ID = atomic.AddUint32(&collectorCounter, 1)
}

// Appengine will replace the Collector's backend http.Client
// With an Http.Client that is provided by appengine/urlfetch
// This function should be used when the scraper is run on
// Google App Engine. Example:
//   func startScraper(w http.ResponseWriter, r *http.Request) {
//     ctx := appengine.NewContext(r)
//     c := colly.NewCollector()
//     c.Appengine(ctx)
//      ...
//     c.Visit("https://google.ca")
//   }
func (c *Collector) Appengine(ctx context.Context) {
	client := urlfetch.Client(ctx)
	client.Jar = c.backend.Client.Jar
	client.CheckRedirect = c.backend.Client.CheckRedirect
	client.Timeout = c.backend.Client.Timeout

	c.backend.Client = client
}

// Visit starts Collector's collecting job by creating a
// request to the URL specified in parameter.
// Visit also calls the previously provided callbacks
func (c *Collector) Visit(URL string) error {
	return c.VisitWithContext(context.Background(), URL)
}

// VisitWithContext functions just like Visit but with
// the ability to pass in a context.Context that is monitored
// as Collector's On* methods are performed. Allowing
// for the halting of a Collector context cancellation.
func (c *Collector) VisitWithContext(ctx context.Context, URL string) error {
	return c.scrape(ctx, URL, "GET", 1, nil, nil, nil, true)
}

// Post starts a collector job by creating a POST request.
// Post also calls the previously provided callbacks
func (c *Collector) Post(URL string, requestData map[string]string) error {
	return c.scrape(context.Background(), URL, "POST", 1, createFormReader(requestData), nil, nil, true)
}

// PostRaw starts a collector job by creating a POST request with raw binary data.
// Post also calls the previously provided callbacks
func (c *Collector) PostRaw(URL string, requestData []byte) error {
	return c.scrape(context.Background(), URL, "POST", 1, bytes.NewReader(requestData), nil, nil, true)
}

// PostMultipart starts a collector job by creating a Multipart POST request
// with raw binary data.  PostMultipart also calls the previously provided callbacks
func (c *Collector) PostMultipart(URL string, requestData map[string][]byte) error {
	boundary := randomBoundary()
	hdr := http.Header{}
	hdr.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	hdr.Set("User-Agent", c.UserAgent)
	return c.scrape(context.Background(), URL, "POST", 1, createMultipartReader(boundary, requestData), nil, hdr, true)
}

// Request starts a collector job by creating a custom HTTP request
// where method, context, headers and request data can be specified.
// Set requestData, ctx, hdr parameters to nil if you don't want to use them.
// Valid methods:
//   - "GET"
//   - "POST"
//   - "PUT"
//   - "DELETE"
//   - "PATCH"
//   - "OPTIONS"
func (c *Collector) Request(method, URL string, requestData io.Reader, ctx *Context, hdr http.Header) error {
	return c.scrape(context.Background(), URL, method, 1, requestData, ctx, hdr, true)
}

// SetDebugger attaches a debugger to the collector
func (c *Collector) SetDebugger(d debug.Debugger) {
	d.Init()
	c.debugger = d
}

// UnmarshalRequest creates a Request from serialized data
func (c *Collector) UnmarshalRequest(r []byte) (*Request, error) {
	req := &serializableRequest{}
	err := json.Unmarshal(r, req)
	if err != nil {
		return nil, err
	}

	u, err := url.Parse(req.URL)
	if err != nil {
		return nil, err
	}

	ctx := NewContext()
	for k, v := range req.Ctx {
		ctx.Put(k, v)
	}

	return &Request{
		Method:    req.Method,
		URL:       u,
		Body:      bytes.NewReader(req.Body),
		Ctx:       ctx,
		ID:        atomic.AddUint32(&c.requestCount, 1),
		Headers:   &req.Headers,
		collector: c,
	}, nil
}

func (c *Collector) scrape(callCtx context.Context, u, method string, depth int, requestData io.Reader, reqCtx *Context, hdr http.Header, checkRevisit bool) error {
	if err := c.requestCheck(u, method, depth, checkRevisit); err != nil {
		return err
	}
	parsedURL, err := url.Parse(u)
	if err != nil {
		return err
	}
	if parsedURL.Scheme == "" {
		parsedURL.Scheme = "http"
	}
	if !c.isDomainAllowed(parsedURL.Host) {
		return ErrForbiddenDomain
	}
	if !c.IgnoreRobotsTxt {
		if err = c.checkRobots(parsedURL); err != nil {
			return err
		}
	}
	if hdr == nil {
		hdr = http.Header{"User-Agent": []string{c.UserAgent}}
	}
	rc, ok := requestData.(io.ReadCloser)
	if !ok && requestData != nil {
		rc = ioutil.NopCloser(requestData)
	}
	req := &http.Request{
		Method:     method,
		URL:        parsedURL,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     hdr,
		Body:       rc,
		Host:       parsedURL.Host,
	}
	setRequestBody(req, requestData)
	u = parsedURL.String()
	c.wg.Add(1)
	if c.Async {
		go c.fetch(callCtx, u, method, depth, requestData, reqCtx, hdr, req)
		return nil
	}
	return c.fetch(callCtx, u, method, depth, requestData, reqCtx, hdr, req)
}

func setRequestBody(req *http.Request, body io.Reader) {
	if body != nil {
		switch v := body.(type) {
		case *bytes.Buffer:
			req.ContentLength = int64(v.Len())
			buf := v.Bytes()
			req.GetBody = func() (io.ReadCloser, error) {
				r := bytes.NewReader(buf)
				return ioutil.NopCloser(r), nil
			}
		case *bytes.Reader:
			req.ContentLength = int64(v.Len())
			snapshot := *v
			req.GetBody = func() (io.ReadCloser, error) {
				r := snapshot
				return ioutil.NopCloser(&r), nil
			}
		case *strings.Reader:
			req.ContentLength = int64(v.Len())
			snapshot := *v
			req.GetBody = func() (io.ReadCloser, error) {
				r := snapshot
				return ioutil.NopCloser(&r), nil
			}
		}
		if req.GetBody != nil && req.ContentLength == 0 {
			req.Body = http.NoBody
			req.GetBody = func() (io.ReadCloser, error) { return http.NoBody, nil }
		}
	}
}

func (c *Collector) fetch(callCtx context.Context, u, method string, depth int, requestData io.Reader, reqCtx *Context, hdr http.Header, req *http.Request) error {
	defer c.wg.Done()
	if reqCtx == nil {
		reqCtx = NewContext()
	}
	request := &Request{
		URL:       req.URL,
		Headers:   &req.Header,
		Ctx:       reqCtx,
		Depth:     depth,
		Method:    method,
		Body:      requestData,
		collector: c,
		ID:        atomic.AddUint32(&c.requestCount, 1),
	}

	c.handleOnRequest(callCtx, request)
	select {
	case <-callCtx.Done():
		return callCtx.Err()
	default:
	}

	if request.abort {
		return nil
	}

	if method == "POST" && req.Header.Get("Content-Type") == "" {
		req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	}

	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "*/*")
	}

	origURL := req.URL
	response, err := c.backend.Cache(req, c.MaxBodySize, c.CacheDir)
	if err := c.handleOnError(callCtx, response, err, request, reqCtx); err != nil {
		return err
	}
	if req.URL != origURL {
		request.URL = req.URL
		request.Headers = &req.Header
	}
	if proxyURL, ok := req.Context().Value(ProxyURLKey).(string); ok {
		request.ProxyURL = proxyURL
	}
	atomic.AddUint32(&c.responseCount, 1)
	response.Ctx = reqCtx
	response.Request = request

	err = response.fixCharset(c.DetectCharset, request.ResponseCharacterEncoding)
	if err != nil {
		return err
	}

	c.handleOnResponse(callCtx, response)
	select {
	case <-callCtx.Done():
		return callCtx.Err()
	default:
	}

	err = c.handleOnHTML(callCtx, response)
	if err != nil {
		c.handleOnError(callCtx, response, err, request, reqCtx)
	}
	select {
	case <-callCtx.Done():
		return callCtx.Err()
	default:
	}

	err = c.handleOnXML(callCtx, response)
	if err != nil {
		c.handleOnError(callCtx, response, err, request, reqCtx)
	}
	select {
	case <-callCtx.Done():
		return callCtx.Err()
	default:
	}

	c.handleOnScraped(callCtx, response)
	select {
	case <-callCtx.Done():
		return callCtx.Err()
	default:
	}

	return err
}

func (c *Collector) requestCheck(u, method string, depth int, checkRevisit bool) error {
	if u == "" {
		return ErrMissingURL
	}
	if c.MaxDepth > 0 && c.MaxDepth < depth {
		return ErrMaxDepth
	}
	if len(c.DisallowedURLFilters) > 0 {
		if isMatchingFilter(c.DisallowedURLFilters, []byte(u)) {
			return ErrForbiddenURL
		}
	}
	if len(c.URLFilters) > 0 {
		if !isMatchingFilter(c.URLFilters, []byte(u)) {
			return ErrNoURLFiltersMatch
		}
	}
	if checkRevisit && !c.AllowURLRevisit && method == "GET" {
		h := fnv.New64a()
		h.Write([]byte(u))
		uHash := h.Sum64()
		visited, err := c.store.IsVisited(uHash)
		if err != nil {
			return err
		}
		if visited {
			return ErrAlreadyVisited
		}
		return c.store.Visited(uHash)
	}
	return nil
}

func (c *Collector) isDomainAllowed(domain string) bool {
	for _, d2 := range c.DisallowedDomains {
		if d2 == domain {
			return false
		}
	}
	if c.AllowedDomains == nil || len(c.AllowedDomains) == 0 {
		return true
	}
	for _, d2 := range c.AllowedDomains {
		if d2 == domain {
			return true
		}
	}
	return false
}

func (c *Collector) checkRobots(u *url.URL) error {
	c.lock.RLock()
	robot, ok := c.robotsMap[u.Host]
	c.lock.RUnlock()

	if !ok {
		// no robots file cached
		resp, err := c.backend.Client.Get(u.Scheme + "://" + u.Host + "/robots.txt")
		if err != nil {
			return err
		}
		robot, err = robotstxt.FromResponse(resp)
		if err != nil {
			return err
		}
		c.lock.Lock()
		c.robotsMap[u.Host] = robot
		c.lock.Unlock()
	}

	uaGroup := robot.FindGroup(c.UserAgent)
	if uaGroup == nil {
		return nil
	}

	if !uaGroup.Test(u.EscapedPath()) {
		return ErrRobotsTxtBlocked
	}
	return nil
}

// String is the text representation of the collector.
// It contains useful debug information about the collector's internals
func (c *Collector) String() string {
	return fmt.Sprintf(
		"Requests made: %d (%d responses) | Callbacks: OnRequest: %d, OnHTML: %d, OnResponse: %d, OnError: %d",
		c.requestCount,
		c.responseCount,
		len(c.requestCallbacks),
		len(c.htmlCallbacks),
		len(c.responseCallbacks),
		len(c.errorCallbacks),
	)
}

// Wait returns when the collector jobs are finished
func (c *Collector) Wait() {
	c.wg.Wait()
}

// OnRequest registers a function. Function will be executed on every
// request made by the Collector
func (c *Collector) OnRequest(f RequestCallback) {
	c.lock.Lock()
	if c.requestCallbacks == nil {
		c.requestCallbacks = make([]RequestCallback, 0, 4)
	}
	c.requestCallbacks = append(c.requestCallbacks, f)
	c.lock.Unlock()
}

// OnResponse registers a function. Function will be executed on every response
func (c *Collector) OnResponse(f ResponseCallback) {
	c.lock.Lock()
	if c.responseCallbacks == nil {
		c.responseCallbacks = make([]ResponseCallback, 0, 4)
	}
	c.responseCallbacks = append(c.responseCallbacks, f)
	c.lock.Unlock()
}

// OnHTML registers a function. Function will be executed on every HTML
// element matched by the GoQuery Selector parameter.
// GoQuery Selector is a selector used by https://github.com/PuerkitoBio/goquery
func (c *Collector) OnHTML(goquerySelector string, f HTMLCallback) {
	c.lock.Lock()
	if c.htmlCallbacks == nil {
		c.htmlCallbacks = make([]*htmlCallbackContainer, 0, 4)
	}
	c.htmlCallbacks = append(c.htmlCallbacks, &htmlCallbackContainer{
		Selector: goquerySelector,
		Function: f,
	})
	c.lock.Unlock()
}

// OnXML registers a function. Function will be executed on every XML
// element matched by the xpath Query parameter.
// xpath Query is used by https://github.com/antchfx/xmlquery
func (c *Collector) OnXML(xpathQuery string, f XMLCallback) {
	c.lock.Lock()
	if c.xmlCallbacks == nil {
		c.xmlCallbacks = make([]*xmlCallbackContainer, 0, 4)
	}
	c.xmlCallbacks = append(c.xmlCallbacks, &xmlCallbackContainer{
		Query:    xpathQuery,
		Function: f,
	})
	c.lock.Unlock()
}

// OnHTMLDetach deregister a function. Function will not be execute after detached
func (c *Collector) OnHTMLDetach(goquerySelector string) {
	c.lock.Lock()
	deleteIdx := -1
	for i, cc := range c.htmlCallbacks {
		if cc.Selector == goquerySelector {
			deleteIdx = i
			break
		}
	}
	if deleteIdx != -1 {
		c.htmlCallbacks = append(c.htmlCallbacks[:deleteIdx], c.htmlCallbacks[deleteIdx+1:]...)
	}
	c.lock.Unlock()
}

// OnXMLDetach deregister a function. Function will not be execute after detached
func (c *Collector) OnXMLDetach(xpathQuery string) {
	c.lock.Lock()
	deleteIdx := -1
	for i, cc := range c.xmlCallbacks {
		if cc.Query == xpathQuery {
			deleteIdx = i
			break
		}
	}
	if deleteIdx != -1 {
		c.xmlCallbacks = append(c.xmlCallbacks[:deleteIdx], c.xmlCallbacks[deleteIdx+1:]...)
	}
	c.lock.Unlock()
}

// OnError registers a function. Function will be executed if an error
// occurs during the HTTP request.
func (c *Collector) OnError(f ErrorCallback) {
	c.lock.Lock()
	if c.errorCallbacks == nil {
		c.errorCallbacks = make([]ErrorCallback, 0, 4)
	}
	c.errorCallbacks = append(c.errorCallbacks, f)
	c.lock.Unlock()
}

// OnScraped registers a function. Function will be executed after
// OnHTML, as a final part of the scraping.
func (c *Collector) OnScraped(f ScrapedCallback) {
	c.lock.Lock()
	if c.scrapedCallbacks == nil {
		c.scrapedCallbacks = make([]ScrapedCallback, 0, 4)
	}
	c.scrapedCallbacks = append(c.scrapedCallbacks, f)
	c.lock.Unlock()
}

// WithTransport allows you to set a custom http.RoundTripper (transport)
func (c *Collector) WithTransport(transport http.RoundTripper) {
	c.backend.Client.Transport = transport
}

// DisableCookies turns off cookie handling
func (c *Collector) DisableCookies() {
	c.backend.Client.Jar = nil
}

// SetCookieJar overrides the previously set cookie jar
func (c *Collector) SetCookieJar(j *cookiejar.Jar) {
	c.backend.Client.Jar = j
}

// SetRequestTimeout overrides the default timeout (10 seconds) for this collector
func (c *Collector) SetRequestTimeout(timeout time.Duration) {
	c.backend.Client.Timeout = timeout
}

// SetStorage overrides the default in-memory storage.
// Storage stores scraping related data like cookies and visited urls
func (c *Collector) SetStorage(s storage.Storage) error {
	if err := s.Init(); err != nil {
		return err
	}
	c.store = s
	c.backend.Client.Jar = createJar(s)
	return nil
}

// SetProxy sets a proxy for the collector. This method overrides the previously
// used http.Transport if the type of the transport is not http.RoundTripper.
// The proxy type is determined by the URL scheme. "http"
// and "socks5" are supported. If the scheme is empty,
// "http" is assumed.
func (c *Collector) SetProxy(proxyURL string) error {
	proxyParsed, err := url.Parse(proxyURL)
	if err != nil {
		return err
	}

	c.SetProxyFunc(http.ProxyURL(proxyParsed))

	return nil
}

// SetProxyFunc sets a custom proxy setter/switcher function.
// See built-in ProxyFuncs for more details.
// This method overrides the previously used http.Transport
// if the type of the transport is not http.RoundTripper.
// The proxy type is determined by the URL scheme. "http"
// and "socks5" are supported. If the scheme is empty,
// "http" is assumed.
func (c *Collector) SetProxyFunc(p ProxyFunc) {
	t, ok := c.backend.Client.Transport.(*http.Transport)
	if c.backend.Client.Transport != nil && ok {
		t.Proxy = p
	} else {
		c.backend.Client.Transport = &http.Transport{
			Proxy: p,
		}
	}
}

func createEvent(eventType string, requestID, collectorID uint32, kvargs map[string]string) *debug.Event {
	return &debug.Event{
		CollectorID: collectorID,
		RequestID:   requestID,
		Type:        eventType,
		Values:      kvargs,
	}
}

func (c *Collector) handleOnRequest(ctx context.Context, r *Request) {
	if c.debugger != nil {
		c.debugger.Event(createEvent("request", r.ID, c.ID, map[string]string{
			"url": r.URL.String(),
		}))
	}
	for _, f := range c.requestCallbacks {
		select {
		case <-ctx.Done():
			return
		default:
			f(ctx, r)
		}
	}
}

func (c *Collector) handleOnResponse(ctx context.Context, r *Response) {
	if c.debugger != nil {
		c.debugger.Event(createEvent("response", r.Request.ID, c.ID, map[string]string{
			"url":    r.Request.URL.String(),
			"status": http.StatusText(r.StatusCode),
		}))
	}
	for _, f := range c.responseCallbacks {
		select {
		case <-ctx.Done():
			return
		default:
			f(ctx, r)
		}
	}
}

func (c *Collector) handleOnHTML(ctx context.Context, resp *Response) error {
	if len(c.htmlCallbacks) == 0 || !strings.Contains(strings.ToLower(resp.Headers.Get("Content-Type")), "html") {
		return nil
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewBuffer(resp.Body))
	if err != nil {
		return err
	}
	if href, found := doc.Find("base[href]").Attr("href"); found {
		resp.Request.baseURL, _ = url.Parse(href)
	}

	var exit bool
	for _, cc := range c.htmlCallbacks {
		i := 0
		doc.Find(cc.Selector).Each(func(_ int, s *goquery.Selection) {
			if exit {
				return
			}

			for _, n := range s.Nodes {
				e := NewHTMLElementFromSelectionNode(resp, s, n, i)
				i++
				if c.debugger != nil {
					c.debugger.Event(createEvent("html", resp.Request.ID, c.ID, map[string]string{
						"selector": cc.Selector,
						"url":      resp.Request.URL.String(),
					}))
				}
				select {
				case <-ctx.Done():
					exit = true
				default:
					cc.Function(ctx, e)
				}
				if exit {
					return
				}
			}
		})
		if exit {
			break
		}
	}
	return nil
}

func (c *Collector) handleOnXML(ctx context.Context, resp *Response) error {
	if len(c.xmlCallbacks) == 0 {
		return nil
	}
	contentType := strings.ToLower(resp.Headers.Get("Content-Type"))
	if !strings.Contains(contentType, "html") && !strings.Contains(contentType, "xml") {
		return nil
	}

	if strings.Contains(contentType, "html") {
		doc, err := htmlquery.Parse(bytes.NewBuffer(resp.Body))
		if err != nil {
			return err
		}
		if e := htmlquery.FindOne(doc, "//base/@href"); e != nil {
			for _, a := range e.Attr {
				if a.Key == "href" {
					resp.Request.baseURL, _ = url.Parse(a.Val)
					break
				}
			}
		}

		var exit bool
		for _, cc := range c.xmlCallbacks {
			htmlquery.FindEach(doc, cc.Query, func(i int, n *html.Node) {
				if exit {
					return
				}

				e := NewXMLElementFromHTMLNode(resp, n)
				if c.debugger != nil {
					c.debugger.Event(createEvent("xml", resp.Request.ID, c.ID, map[string]string{
						"selector": cc.Query,
						"url":      resp.Request.URL.String(),
					}))
				}
				select {
				case <-ctx.Done():
					exit = true
				default:
					cc.Function(ctx, e)
				}
			})
			if exit {
				break
			}
		}
	} else if strings.Contains(contentType, "xml") {
		doc, err := xmlquery.Parse(bytes.NewBuffer(resp.Body))
		if err != nil {
			return err
		}

		var exit bool
		for _, cc := range c.xmlCallbacks {
			xmlquery.FindEach(doc, cc.Query, func(i int, n *xmlquery.Node) {
				if exit {
					return
				}

				e := NewXMLElementFromXMLNode(resp, n)
				if c.debugger != nil {
					c.debugger.Event(createEvent("xml", resp.Request.ID, c.ID, map[string]string{
						"selector": cc.Query,
						"url":      resp.Request.URL.String(),
					}))
				}
				select {
				case <-ctx.Done():
					exit = true
				default:
					cc.Function(ctx, e)
				}
			})
			if exit {
				break
			}
		}
	}
	return nil
}

func (c *Collector) handleOnError(callCtx context.Context, response *Response, err error, request *Request, ctx *Context) error {
	if err == nil && (c.ParseHTTPErrorResponse || response.StatusCode < 203) {
		return nil
	}
	if err == nil && response.StatusCode >= 203 {
		err = errors.New(http.StatusText(response.StatusCode))
	}
	if response == nil {
		response = &Response{
			Request: request,
			Ctx:     ctx,
		}
	}
	if c.debugger != nil {
		c.debugger.Event(createEvent("error", request.ID, c.ID, map[string]string{
			"url":    request.URL.String(),
			"status": http.StatusText(response.StatusCode),
		}))
	}
	if response.Request == nil {
		response.Request = request
	}
	if response.Ctx == nil {
		response.Ctx = request.Ctx
	}
	for _, f := range c.errorCallbacks {
		select {
		case <-callCtx.Done():
			return err
		default:
			f(callCtx, response, err)
		}
	}
	return err
}

func (c *Collector) handleOnScraped(ctx context.Context, r *Response) {
	if c.debugger != nil {
		c.debugger.Event(createEvent("scraped", r.Request.ID, c.ID, map[string]string{
			"url": r.Request.URL.String(),
		}))
	}
	for _, f := range c.scrapedCallbacks {
		select {
		case <-ctx.Done():
			return
		default:
			f(ctx, r)
		}
	}
}

// Limit adds a new LimitRule to the collector
func (c *Collector) Limit(rule *LimitRule) error {
	return c.backend.Limit(rule)
}

// Limits adds new LimitRules to the collector
func (c *Collector) Limits(rules []*LimitRule) error {
	return c.backend.Limits(rules)
}

// SetCookies handles the receipt of the cookies in a reply for the given URL
func (c *Collector) SetCookies(URL string, cookies []*http.Cookie) error {
	if c.backend.Client.Jar == nil {
		return ErrNoCookieJar
	}
	u, err := url.Parse(URL)
	if err != nil {
		return err
	}
	c.backend.Client.Jar.SetCookies(u, cookies)
	return nil
}

// Cookies returns the cookies to send in a request for the given URL.
func (c *Collector) Cookies(URL string) []*http.Cookie {
	if c.backend.Client.Jar == nil {
		return nil
	}
	u, err := url.Parse(URL)
	if err != nil {
		return nil
	}
	return c.backend.Client.Jar.Cookies(u)
}

// Clone creates an exact copy of a Collector without callbacks.
// HTTP backend, robots.txt cache and cookie jar are shared
// between collectors.
func (c *Collector) Clone() *Collector {
	return &Collector{
		AllowedDomains:         c.AllowedDomains,
		AllowURLRevisit:        c.AllowURLRevisit,
		CacheDir:               c.CacheDir,
		DetectCharset:          c.DetectCharset,
		DisallowedDomains:      c.DisallowedDomains,
		ID:                     atomic.AddUint32(&collectorCounter, 1),
		IgnoreRobotsTxt:        c.IgnoreRobotsTxt,
		MaxBodySize:            c.MaxBodySize,
		MaxDepth:               c.MaxDepth,
		DisallowedURLFilters:   c.DisallowedURLFilters,
		URLFilters:             c.URLFilters,
		ParseHTTPErrorResponse: c.ParseHTTPErrorResponse,
		UserAgent:              c.UserAgent,
		store:                  c.store,
		backend:                c.backend,
		debugger:               c.debugger,
		Async:                  c.Async,
		RedirectHandler:        c.RedirectHandler,
		errorCallbacks:         make([]ErrorCallback, 0, 8),
		htmlCallbacks:          make([]*htmlCallbackContainer, 0, 8),
		xmlCallbacks:           make([]*xmlCallbackContainer, 0, 8),
		scrapedCallbacks:       make([]ScrapedCallback, 0, 8),
		lock:                   c.lock,
		requestCallbacks:       make([]RequestCallback, 0, 8),
		responseCallbacks:      make([]ResponseCallback, 0, 8),
		robotsMap:              c.robotsMap,
		wg:                     &sync.WaitGroup{},
	}
}

func (c *Collector) checkRedirectFunc() func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if !c.isDomainAllowed(req.URL.Host) {
			return fmt.Errorf("Not following redirect to %s because its not in AllowedDomains", req.URL.Host)
		}

		if c.RedirectHandler != nil {
			return c.RedirectHandler(req, via)
		}

		// Honor golangs default of maximum of 10 redirects
		if len(via) >= 10 {
			return http.ErrUseLastResponse
		}

		lastRequest := via[len(via)-1]

		// Copy the headers from last request
		for hName, hValues := range lastRequest.Header {
			for _, hValue := range hValues {
				req.Header.Set(hName, hValue)
			}
		}

		// If domain has changed, remove the Authorization-header if it exists
		if req.URL.Host != lastRequest.URL.Host {
			req.Header.Del("Authorization")
		}

		return nil
	}
}

func (c *Collector) parseSettingsFromEnv() {
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "COLLY_") {
			continue
		}
		pair := strings.SplitN(e[6:], "=", 2)
		if f, ok := envMap[pair[0]]; ok {
			f(c, pair[1])
		} else {
			log.Println("Unknown environment variable:", pair[0])
		}
	}
}

// SanitizeFileName replaces dangerous characters in a string
// so the return value can be used as a safe file name.
func SanitizeFileName(fileName string) string {
	ext := filepath.Ext(fileName)
	cleanExt := sanitize.BaseName(ext)
	if cleanExt == "" {
		cleanExt = ".unknown"
	}
	return strings.Replace(fmt.Sprintf(
		"%s.%s",
		sanitize.BaseName(fileName[:len(fileName)-len(ext)]),
		cleanExt[1:],
	), "-", "_", -1)
}

func createFormReader(data map[string]string) io.Reader {
	form := url.Values{}
	for k, v := range data {
		form.Add(k, v)
	}
	return strings.NewReader(form.Encode())
}

func createMultipartReader(boundary string, data map[string][]byte) io.Reader {
	dashBoundary := "--" + boundary

	body := []byte{}
	buffer := bytes.NewBuffer(body)

	buffer.WriteString("Content-type: multipart/form-data; boundary=" + boundary + "\n\n")
	for contentType, content := range data {
		buffer.WriteString(dashBoundary + "\n")
		buffer.WriteString("Content-Disposition: form-data; name=" + contentType + "\n")
		buffer.WriteString(fmt.Sprintf("Content-Length: %d \n\n", len(content)))
		buffer.Write(content)
		buffer.WriteString("\n")
	}
	buffer.WriteString(dashBoundary + "--\n\n")
	return buffer
}

// randomBoundary was borrowed from
// github.com/golang/go/mime/multipart/writer.go#randomBoundary
func randomBoundary() string {
	var buf [30]byte
	_, err := io.ReadFull(rand.Reader, buf[:])
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", buf[:])
}

func isYesString(s string) bool {
	switch strings.ToLower(s) {
	case "1", "yes", "true", "y":
		return true
	}
	return false
}

func createJar(s storage.Storage) http.CookieJar {
	return &cookieJarSerializer{store: s, lock: &sync.RWMutex{}}
}

func (j *cookieJarSerializer) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.lock.Lock()
	defer j.lock.Unlock()
	cookieStr := j.store.Cookies(u)

	// Merge existing cookies, new cookies have precedence.
	cnew := make([]*http.Cookie, len(cookies))
	copy(cnew, cookies)
	existing := storage.UnstringifyCookies(cookieStr)
	for _, c := range existing {
		if !storage.ContainsCookie(cnew, c.Name) {
			cnew = append(cnew, c)
		}
	}
	j.store.SetCookies(u, storage.StringifyCookies(cnew))
}

func (j *cookieJarSerializer) Cookies(u *url.URL) []*http.Cookie {
	cookies := storage.UnstringifyCookies(j.store.Cookies(u))
	// Filter.
	now := time.Now()
	cnew := make([]*http.Cookie, 0, len(cookies))
	for _, c := range cookies {
		// Drop expired cookies.
		if c.RawExpires != "" && c.Expires.Before(now) {
			continue
		}
		// Drop secure cookies if not over https.
		if c.Secure && u.Scheme != "https" {
			continue
		}
		cnew = append(cnew, c)
	}
	return cnew
}

func isMatchingFilter(fs []*regexp.Regexp, d []byte) bool {
	for _, r := range fs {
		if r.Match(d) {
			return true
		}
	}
	return false
}
