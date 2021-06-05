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

// ResetRequestCall remove all registered function.
func (c *Collector) ResetRequestCall() {
	c.lock.Lock()
	if c.requestCallbacks != nil {
		c.requestCallbacks = make([]RequestCallback, 0, 4)
	}
	c.lock.Unlock()
}

// ResetResponseCall remove all registered function.
func (c *Collector) ResetResponseCall() {
	c.lock.Lock()
	if c.responseCallbacks != nil {
		c.responseCallbacks = make([]ResponseCallback, 0, 4)
	}
	c.lock.Unlock()
}

// ResetResponseHeadersCall remove all registered function.
func (c *Collector) ResetResponseHeadersCall() {
	c.lock.Lock()
	if c.responseHeadersCallbacks != nil {
		c.responseHeadersCallbacks = make([]ResponseHeadersCallback, 0, 4)
	}
	c.lock.Unlock()
}

// ResetHtmlCall remove all registered function.
func (c *Collector) ResetHtmlCall() {
	c.lock.Lock()
	if c.htmlCallbacks != nil {
		c.htmlCallbacks = make([]*htmlCallbackContainer, 0, 4)
	}
	c.lock.Unlock()
}

// ResetXMLCall remove all registered function.
func (c *Collector) ResetXMLCall() {
	c.lock.Lock()
	if c.xmlCallbacks != nil {
		c.xmlCallbacks = make([]*xmlCallbackContainer, 0, 4)
	}
	c.lock.Unlock()
}
