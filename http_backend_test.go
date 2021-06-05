package colly

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHTTPBackendDoCancelation(t *testing.T) {
	// hardcoded fixed to ensure that p, n, c are the same making the test more reliable
	rand.Seed(2927496558871806)

	// rand up to 10 to not extend the test duration too much
	p := 1 + rand.Intn(5)        // p: parallel requests
	n := p + p*rand.Intn(10)     // n: after n, cancel will be called; ensure 1 calls per worker + rand
	c := n + p*2 + rand.Intn(10) // c: total number of calls; ensure 2 calls per worker after cancel is called + rand

	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		fmt.Fprint(rw, "OK")
	}))
	defer ts.Close()

	checkHeadersFunc := func(req *http.Request, statusCode int, header http.Header) bool { return true }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	backend := &httpBackend{}
	jar, _ := cookiejar.New(nil)
	backend.Init(jar)
	limit := &LimitRule{
		DomainRegexp: ".*",
		Parallelism:  p,
		Delay:        5 * time.Millisecond,
	}
	backend.Limit(limit)

	var wg sync.WaitGroup
	wg.Add(c)

	out := make(chan error)

	for i := 0; i < c; i++ {
		go func(i int) {
			defer wg.Done()
			req, _ := http.NewRequest("GET", ts.URL+"/"+strconv.Itoa(i), nil)
			req = req.WithContext(ctx)

			_, err := backend.Do(req, 0, checkHeadersFunc)
			out <- err
		}(i)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	i := 0
	nonEarlyCount := 0
	for err := range out {
		i++
		if i == n {
			cancel()
		}

		if i <= n {
			if err != nil {
				t.Errorf("no error was expected for the first %d responses; error: %q", n, err)
			}
		} else {
			errStr := ""
			if err != nil {
				errStr = err.Error()
			}

			// non early returns are allowed up to the number of maximum allowed concurrent requests;
			// bacause those requests could be already running when cancel was called
			if !strings.Contains(errStr, "early return") {
				if nonEarlyCount > p {
					t.Errorf("count of non early return is above the number of maximum allowed concurrent requests; p: %d; n: %d; c: %d", p, n, c)
				}
				nonEarlyCount++
			}
		}
	}
}
