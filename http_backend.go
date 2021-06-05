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

package colly

import (
	"crypto/sha1"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"compress/gzip"

	"github.com/gobwas/glob"
)

type httpBackend struct {
	LimitRules []*LimitRule
	Client     *http.Client
	lock       *sync.RWMutex
}

type checkHeadersFunc func(req *http.Request, statusCode int, header http.Header) bool

// LimitRule provides connection restrictions for domains.
// Both DomainRegexp and DomainGlob can be used to specify
// the included domains patterns, but at least one is required.
// There can be two kind of limitations:
//  - Parallelism: Set limit for the number of concurrent requests to matching domains
//  - Delay: Wait specified amount of time between requests (parallelism is 1 in this case)
type LimitRule struct {
	// DomainRegexp is a regular expression to match against domains
	DomainRegexp string
	// DomainGlob is a glob pattern to match against domains
	DomainGlob string
	// Delay is the duration to wait before creating a new request to the matching domains
	Delay time.Duration
	// RandomDelay is the extra randomized duration to wait added to Delay before creating a new request
	RandomDelay time.Duration
	// Parallelism is the number of the maximum allowed concurrent requests of the matching domains
	Parallelism    int
	waitChan       chan bool
	compiledRegexp *regexp.Regexp
	compiledGlob   glob.Glob
}

// Init initializes the private members of LimitRule
func (r *LimitRule) Init() error {
	waitChanSize := 1
	if r.Parallelism > 1 {
		waitChanSize = r.Parallelism
	}
	r.waitChan = make(chan bool, waitChanSize)
	hasPattern := false
	if r.DomainRegexp != "" {
		c, err := regexp.Compile(r.DomainRegexp)
		if err != nil {
			return err
		}
		r.compiledRegexp = c
		hasPattern = true
	}
	if r.DomainGlob != "" {
		c, err := glob.Compile(r.DomainGlob)
		if err != nil {
			return err
		}
		r.compiledGlob = c
		hasPattern = true
	}
	if !hasPattern {
		return ErrNoPattern
	}
	return nil
}

func (h *httpBackend) Init(jar http.CookieJar) {
	rand.Seed(time.Now().UnixNano())
	h.Client = &http.Client{
		Jar:     jar,
		Timeout: 10 * time.Second,
	}
	h.lock = &sync.RWMutex{}
}

// Match checks that the domain parameter triggers the rule
func (r *LimitRule) Match(domain string) bool {
	match := false
	if r.compiledRegexp != nil && r.compiledRegexp.MatchString(domain) {
		match = true
	}
	if r.compiledGlob != nil && r.compiledGlob.Match(domain) {
		match = true
	}
	return match
}

func (h *httpBackend) GetMatchingRule(domain string) *LimitRule {
	if h.LimitRules == nil {
		return nil
	}
	h.lock.RLock()
	defer h.lock.RUnlock()
	for _, r := range h.LimitRules {
		if r.Match(domain) {
			return r
		}
	}
	return nil
}

func (h *httpBackend) Cache(request *http.Request, bodySize int, checkHeadersFunc checkHeadersFunc, cacheDir string) (*Response, error) {
	if cacheDir == "" || request.Method != "GET" || request.Header.Get("Cache-Control") == "no-cache" {
		return h.Do(request, bodySize, checkHeadersFunc)
	}
	sum := sha1.Sum([]byte(request.URL.String()))
	hash := hex.EncodeToString(sum[:])
	dir := path.Join(cacheDir, hash[:2])
	filename := path.Join(dir, hash)
	if file, err := os.Open(filename); err == nil {
		resp := new(Response)
		err := gob.NewDecoder(file).Decode(resp)
		file.Close()
		checkHeadersFunc(request, resp.StatusCode, *resp.Headers)
		if resp.StatusCode < 500 {
			return resp, err
		}
	}
	resp, err := h.Do(request, bodySize, checkHeadersFunc)
	if err != nil || resp.StatusCode >= 500 {
		return resp, err
	}
	if _, err := os.Stat(dir); err != nil {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return resp, err
		}
	}
	file, err := os.Create(filename + "~")
	if err != nil {
		return resp, err
	}
	if err := gob.NewEncoder(file).Encode(resp); err != nil {
		file.Close()
		return resp, err
	}
	file.Close()
	return resp, os.Rename(filename+"~", filename)
}

func (h *httpBackend) Do(request *http.Request, bodySize int, checkHeadersFunc checkHeadersFunc) (*Response, error) {
	r := h.GetMatchingRule(request.URL.Host)
	if r != nil {
		select {
		case r.waitChan <- true:
		case <-request.Context().Done():
			return nil, errors.New("context canceled; early return")
		}

		defer func(r *LimitRule) {
			randomDelay := time.Duration(0)
			if r.RandomDelay != 0 {
				randomDelay = time.Duration(rand.Int63n(int64(r.RandomDelay)))
			}
			time.Sleep(r.Delay + randomDelay)
			<-r.waitChan
		}(r)
	}

	res, err := h.Client.Do(request)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.Request != nil {
		*request = *res.Request
	}
	if !checkHeadersFunc(request, res.StatusCode, res.Header) {
		// closing res.Body (see defer above) without reading it aborts
		// the download
		return nil, ErrAbortedAfterHeaders
	}

	var bodyReader io.Reader = res.Body
	if bodySize > 0 {
		bodyReader = io.LimitReader(bodyReader, int64(bodySize))
	}
	contentEncoding := strings.ToLower(res.Header.Get("Content-Encoding"))
	if !res.Uncompressed && (strings.Contains(contentEncoding, "gzip") || (contentEncoding == "" && strings.Contains(strings.ToLower(res.Header.Get("Content-Type")), "gzip")) || strings.HasSuffix(strings.ToLower(request.URL.Path), ".xml.gz")) {
		bodyReader, err = gzip.NewReader(bodyReader)
		if err != nil {
			return nil, err
		}
		defer bodyReader.(*gzip.Reader).Close()
	}
	body, err := ioutil.ReadAll(bodyReader)
	if err != nil {
		return nil, err
	}
	return &Response{
		StatusCode: res.StatusCode,
		Body:       body,
		Headers:    &res.Header,
	}, nil
}

func (h *httpBackend) Limit(rule *LimitRule) error {
	h.lock.Lock()
	if h.LimitRules == nil {
		h.LimitRules = make([]*LimitRule, 0, 8)
	}
	h.LimitRules = append(h.LimitRules, rule)
	h.lock.Unlock()
	return rule.Init()
}

func (h *httpBackend) Limits(rules []*LimitRule) error {
	for _, r := range rules {
		if err := h.Limit(r); err != nil {
			return err
		}
	}
	return nil
}
