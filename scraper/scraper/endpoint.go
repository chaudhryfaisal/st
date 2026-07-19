package scraper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

//Endpoint represents a single remote endpoint. The performed
//query can be modified between each call by parameterising
//URL. See documentation.
type Endpoint struct {
	Name             string                `json:"name,omitempty"`
	Method           string                `json:"method,omitempty"`
	URL              string                `json:"url"`
	Body             string                `json:"body,omitempty"`
	Headers          map[string]string     `json:"headers,omitempty"`
	List             string                `json:"list,omitempty"`
	Result           map[string]Extractors `json:"result"`
	Debug            bool
	Flare            bool                  `json:"flare,omitempty"`
	JSON             bool                  `json:"json,omitempty"`
	FlareSolverrURL  string
	Client           *http.Client
}

//extract 1 result using this endpoints extractor map
func (e *Endpoint) extract(sel *goquery.Selection) Result {
	r := Result{}
	for field, ext := range e.Result {
		if v := ext.execute(sel); v != "" {
			r[field] = v
		} else if e.Debug {
			logf("missing %s", field)
		}
	}
	return r
}

func jsonExtract(raw interface{}, path string) string {
	parts := strings.Split(path, ".")
	var cur interface{} = raw
	for _, part := range parts {
		if cur == nil {
			return ""
		}
		switch v := cur.(type) {
		case map[string]interface{}:
			cur = v[part]
		case []interface{}:
			idx := 0
			if _, err := fmt.Sscanf(part, "%d", &idx); err != nil || idx < 0 || idx >= len(v) {
				return ""
			}
			cur = v[idx]
		default:
			return ""
		}
	}
	if cur == nil {
		return ""
	}
	return fmt.Sprintf("%v", cur)
}

func (e *Endpoint) extractJSON(raw interface{}) Result {
	r := Result{}
	for field, ext := range e.Result {
		path := ext.JSONPath()
		if path == "" {
			for _, ex := range ext {
				path = ex.val
				if path != "" {
					break
				}
			}
		}
		if path == "" {
			continue
		}
		v := jsonExtract(raw, path)
		if v != "" {
			r[field] = v
		} else if e.Debug {
			logf("missing %s", field)
		}
	}
	return r
}

func (e *Endpoint) doFlareSolverr(reqURL string) (*http.Client, error) {
	if e.FlareSolverrURL == "" {
		return nil, nil
	}
	payload := map[string]interface{}{
		"cmd":                 "request.get",
		"url":                 reqURL,
		"session":             "st",
		"session_ttl_minutes": 5,
		"returnOnlyCookies":   true,
		"disableMedia":        true,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", e.FlareSolverrURL+"/v1", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := e.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var fsResp struct {
		Solution struct {
			Cookies []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"cookies"`
			UserAgent string `json:"userAgent"`
		} `json:"solution"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&fsResp); err != nil {
		return nil, err
	}
	if fsResp.Error != "" {
		return nil, fmt.Errorf("flaresolverr error: %s", fsResp.Error)
	}
	if len(fsResp.Solution.Cookies) == 0 {
		return nil, nil
	}
	jar := &memoryCookieJar{
		cookies: make(map[string][]*http.Cookie),
	}
	targetURL, err := url.Parse(reqURL)
	if err != nil {
		return nil, err
	}
	for _, c := range fsResp.Solution.Cookies {
		jar.cookies[targetURL.String()] = append(jar.cookies[targetURL.String()], &http.Cookie{
			Name:  c.Name,
			Value: c.Value,
		})
	}
	clientWithCookies := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			for _, c := range fsResp.Solution.Cookies {
				req.AddCookie(&http.Cookie{
					Name:  c.Name,
					Value: c.Value,
				})
			}
			return nil
		},
	}
	return clientWithCookies, nil
}

type memoryCookieJar struct {
	cookies map[string][]*http.Cookie
}

func (j *memoryCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.cookies[u.String()] = append(j.cookies[u.String()], cookies...)
}

func (j *memoryCookieJar) Cookies(u *url.URL) []*http.Cookie {
	return j.cookies[u.String()]
}

// Execute will execute an Endpoint with the given params
func (e *Endpoint) Execute(params map[string]string) ([]Result, error) {
	//render url using params
	url, err := template(true, e.URL, params)
	if err != nil {
		return nil, err
	}
	//default method
	method := e.Method
	if method == "" {
		method = "GET"
	}
	//render body (if set)
	body := io.Reader(nil)
	if e.Body != "" {
		s, err := template(true, e.Body, params)
		if err != nil {
			return nil, err
		}
		body = strings.NewReader(s)
		if e.Debug {
			logf("req: %s %s (body size %d)", method, url, len(s))
		}
	} else {
		if e.Debug {
			logf("req: %s %s", method, url)
		}
	}
	//show results
	//create HTTP request
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	h := http.Header{}
	if e.Headers != nil {
		for k, v := range e.Headers {
			h.Set(k, v)
		}
	}
	if e.Debug {
		for k := range h {
			logf("header: %s=%s", k, h.Get(k))
		}
	}
	req.Header = h
	client := e.Client
	if client == nil {
		client = http.DefaultClient
	}
	useFlare := e.Flare && e.FlareSolverrURL != ""
	if useFlare {
		fsClient, err := e.doFlareSolverr(url)
		if err != nil {
			return nil, err
		}
		if fsClient != nil {
			client = fsClient
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if e.Debug {
		logf("resp: %d (type: %s, len: %s)", resp.StatusCode,
			resp.Header.Get("Content-Type"), resp.Header.Get("Content-Length"))
	}
	//results will be either a list of results, or a single result
	var results []Result
	if e.JSON {
		var raw interface{}
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			return nil, err
		}
		switch v := raw.(type) {
		case []interface{}:
			for _, item := range v {
				r := e.extractJSON(item)
				if len(r) > 0 {
					results = append(results, r)
				}
			}
		default:
			results = append(results, e.extractJSON(raw))
		}
		return results, nil
	}
	//parse HTML
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}
	sel := doc.Selection
	if e.List != "" {
		sels := sel.Find(e.List)
		if e.Debug {
			logf("list: %s => #%d elements", e.List, sels.Length())
		}
		if e.Debug && sels.Length() == 0 {
			logf("no results, printing HTML")
			h, _ := sel.Html()
			fmt.Println(h)
		}
		sels.Each(func(i int, sel *goquery.Selection) {
			r := e.extract(sel)
			if len(r) == len(e.Result) {
				results = append(results, r)
			} else if e.Debug {
				logf("excluded #%d: has %d fields, expected %d", i, len(r), len(e.Result))
			}
		})
	} else {
		results = append(results, e.extract(sel))
	}
	return results, nil
}
