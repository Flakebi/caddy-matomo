package matomo

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/mholt/caddy"
	"github.com/mholt/caddy/caddyhttp/httpserver"
)

func init() {
	caddy.RegisterPlugin("matomo", caddy.Plugin{
		ServerType: "http",
		Action:     setup,
	})

	/*httpserver.AddListenerMiddleware(myListenerMiddleware)

	// ... there are others. See the godoc.

	cfg := httpserver.GetConfig(c)
	mid := func(next httpserver.Handler) httpserver.Handler {
		return MyHandler{Next: next}
	}
	cfg.AddMiddleware(mid)*/
}

type MatomoHandler struct {
	Next  httpserver.Handler
	config MatomoHandlerConfig
}

type MatomoHandlerConfig struct {
	Next     httpserver.Handler
	url      string    // Url of Matomo, e.g. http://localhost:2015/piwik.php
	site     string    // Site id, default 1
	token    string    // The access token for Matomo
	bots     bool      // If Matomo should count bots, default true
	excludes []*regexp.Regexp // If one of these expressions matches, the request is not recorded
}

func MakeRequest(req *http.Request) {
	client := &http.Client{}
	_, err := client.Do(req)
	if err != nil {
		log.Println(err)
	}
	// Debug:
	//println(req.URL.String())
}

func (h MatomoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) (int, error) {
	rw := httpserver.NewResponseRecorder(w)
	status, err := h.Next.ServeHTTP(rw, r)

	// Create Matomo request
	req, err := http.NewRequest("GET", h.config.url, nil)
	if err != nil {
		log.Println(err)
	} else {
		u, err := url.ParseRequestURI(r.RequestURI)
		if err != nil {
			log.Println(err)
			u = r.URL
		}
		// Seems like there is no way to get this information
		u.Scheme = "http"
		u.Host = r.Host
		if u.Host == "" {
			u.Host = "example.com"
		}
		request_url := u.String()

		// Check if this url is excluded
		for _, exclude := range h.config.excludes {
			if exclude.MatchString(r.RequestURI) {
				return status, err
			}
		}

		// Get status (taken from caddy-prometheus)
		stat := status
		if err != nil && status == 0 {
			stat = 500
		} else if status == 0 {
			stat = rw.Status()
		}

		q := req.URL.Query()
		q.Add("rec", "1")
		q.Add("apiv", "1")
		q.Add("send_image", "0")

		// Encode status in a page scope custom dimension
		q.Add("dimension1", strconv.Itoa(stat))
		q.Add("url", request_url)
		ind := strings.LastIndex(r.RemoteAddr, ":")
		if ind == -1 {
			log.Println("Cannot find : in RemoteAddr")
			q.Add("cip", r.RemoteAddr)
		} else {
			q.Add("cip", strings.Trim(r.RemoteAddr[:ind], "[]"))
		}

		ref := r.Referer()
		if ref != "" {
			q.Add("urlref", ref)
		}

		agent := r.UserAgent()
		if agent != "" {
			q.Add("ua", agent)
		}

		lang := r.Header.Get("Accept-Language")
		if lang != "" {
			q.Add("lang", lang)
		}

		// Customizable
		q.Add("idsite", h.config.site)
		q.Add("token_auth", h.config.token)
		if h.config.bots {
			q.Add("bots", "1") // Log requests by bots
		}

		req.URL.RawQuery = q.Encode()

		go MakeRequest(req)
	}

	return status, err
}

func setup(c *caddy.Controller) error {
	config := MatomoHandlerConfig{site: "1", bots: true, excludes: make([]*regexp.Regexp, 0)}
	for c.Next() {
		val := c.Val()
		if val == "matomo" {
			// Parse block
			if c.Next() {
				if c.Val() == "{" {
					loop := true
					for loop && c.Next() {
						val := c.Val()
						args := c.RemainingArgs()
						switch val {
						case "url":
							if len(args) == 0 {
								return fmt.Errorf("expecting an argument for \"%s\"", val)
							}
							config.url = args[0]
						case "token":
							if len(args) == 0 {
								return fmt.Errorf("expecting an argument for \"%s\"", val)
							}
							config.token = args[0]
						case "site":
							if len(args) == 0 {
								return fmt.Errorf("expecting an argument for \"%s\"", val)
							}
							config.site = args[0]
						case "exclude":
							if len(args) == 0 {
								return fmt.Errorf("expecting an argument for \"%s\"", val)
							}
							r, err := regexp.Compile(args[0])
							if err != nil {
								log.Printf("Failed to compile exclude regex '%v': %v\n", args[0], err)
							} else {
								config.excludes = append(config.excludes, r)
							}
						case "nobots":
							config.bots = false
						case "}":
							loop = false
						}
					}
				} else {
					return fmt.Errorf("expecting \"{\", got \"%s\"", c.Val())
				}
			} else {
				return fmt.Errorf("expecting braces", val)
			}
		} else {
			return fmt.Errorf("expecting \"matomo\", got \"%s\"", val)
		}
	}

	if config.url == "" {
		return fmt.Errorf("expecting \"url\" attribute for matomo directive")
	}
	if config.token == "" {
		return fmt.Errorf("expecting \"token\" attribute for matomo directive")
	}

	cfg := httpserver.GetConfig(c)
	mid := func(next httpserver.Handler) httpserver.Handler {
		return MatomoHandler{Next: next, config: config}
	}
	cfg.AddMiddleware(mid)

	return nil
}
