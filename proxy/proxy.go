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

package proxy

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"sync/atomic"

	"github.com/gocolly/colly/v2"
)

type roundRobinSwitcher struct {
	proxyURLs []*url.URL
	index     uint32
}

type authenticatedRoundRobinSwitcher struct {
	roundRobinSwitcher colly.ProxyFunc
	username           string
	password           string
}

func (r *roundRobinSwitcher) GetProxy(pr *http.Request) (*url.URL, error) {
	index := atomic.AddUint32(&r.index, 1) - 1
	u := r.proxyURLs[index%uint32(len(r.proxyURLs))]

	ctx := context.WithValue(pr.Context(), colly.ProxyURLKey, u.String())
	*pr = *pr.WithContext(ctx)
	return u, nil
}

func (r *authenticatedRoundRobinSwitcher) GetAuthenticatedProxy(request *http.Request) (*url.URL, error) {
	auth := r.username + ":" + r.password
	basicAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
	request.Header.Add("Proxy-Authorization", basicAuth)
	request.Header.Add("Proxy-Connection", "Keep-Alive")
	url, err := r.roundRobinSwitcher(request)
	if err != nil {
		return nil, err
	}
	return url, nil
}

// RoundRobinProxySwitcher creates a proxy switcher function which rotates
// ProxyURLs on every request.
// The proxy type is determined by the URL scheme. "http", "https"
// and "socks5" are supported. If the scheme is empty,
// "http" is assumed.
func RoundRobinProxySwitcher(ProxyURLs ...string) (colly.ProxyFunc, error) {
	if len(ProxyURLs) < 1 {
		return nil, colly.ErrEmptyProxyURL
	}
	urls := make([]*url.URL, len(ProxyURLs))
	for i, u := range ProxyURLs {
		parsedU, err := url.Parse(u)
		if err != nil {
			return nil, err
		}
		urls[i] = parsedU
	}
	return (&roundRobinSwitcher{urls, 0}).GetProxy, nil
}

// AuthenticatedRoundRobinProxySwitcher decorates RoundRobinProxySwitcher and sets proxy credentials.
// See RoundRobinProxySwitcher for more details on rotating proxies.
// This method sets correct "Proxy-Connection" and "Proxy-Authorization" headers.
// "Proxy-Authorization" contains the credentials (username, password) to authenticate
// a user agent to a proxy server.
func AuthenticatedRoundRobinProxySwitcher(username string, password string, ProxyURLs ...string) (func(*http.Request) (*url.URL, error), error) {
	roundRobinSwitcher, err := RoundRobinProxySwitcher(ProxyURLs...)
	if err != nil {
		return nil, err
	}
	return (&authenticatedRoundRobinSwitcher{roundRobinSwitcher, username, password}).GetAuthenticatedProxy, nil
}
