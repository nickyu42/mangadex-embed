package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/valyala/fastjson"
	"golang.org/x/time/rate"
)

const (
	mangaEndpoint  = "https://api.mangadex.org/manga/%s"
	authorEndpoint = "https://api.mangadex.org/author/%s"
	coverEndpoint  = "https://api.mangadex.org/cover/%s"

	CoverUri = "https://uploads.mangadex.org/covers/%s/%s"
)

var dexClient *RateLimitedClient
var parser fastjson.Parser

type RateLimitedClient struct {
	client      *http.Client
	Ratelimiter *rate.Limiter
}

func (c *RateLimitedClient) Do(req *http.Request) (*http.Response, error) {
	ctx := context.Background()
	err := c.Ratelimiter.Wait(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *RateLimitedClient) RequestJSON(endpoint string, id string) (*fastjson.Value, error) {
	request, _ := http.NewRequest("GET", fmt.Sprintf(endpoint, id), nil)

	var err error
	var resp *http.Response
	if resp, err = c.Do(request); err != nil {
		return nil, fmt.Errorf("could not complete manga request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status not ok: %w", err)
	}

	var bytes []byte
	if bytes, err = io.ReadAll(resp.Body); err != nil {
		return nil, fmt.Errorf("could not unmarshal response: %w", err)
	}

	var val *fastjson.Value
	if val, err = parser.ParseBytes(bytes); err != nil {
		return nil, fmt.Errorf("could not unmarshal response: %w", err)
	}

	return val, nil
}

func newRLClient(rl *rate.Limiter) *RateLimitedClient {
	c := &RateLimitedClient{
		client:      http.DefaultClient,
		Ratelimiter: rl,
	}
	return c
}

func main() {
	// Setup logging
	gin.DisableConsoleColor()
	f, _ := os.OpenFile("gin.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	logOut := io.MultiWriter(f, os.Stdout)
	gin.DefaultWriter = logOut

	// Creat mangadex API client
	createDexClient(logOut)

	// Init GIN router
	r := gin.New()

	// Setup middleware
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	// Setup templates
	r.LoadHTMLGlob("templates/*")

	// Setup routes
	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{})
	})

	r.GET("/title/:md-id", createEmbed)
	r.GET("/title/:md-id/:manga-name", createEmbed)

	r.Run()
}

func createDexClient(logOut io.Writer) {
	rl := rate.NewLimiter(rate.Every(2*time.Second), 5)
	dexClient = newRLClient(rl)
}

func parseMangaResponse(val *fastjson.Value, mangaId string) gin.H {
	attr := val.Get("data").Get("attributes")

	titleObj := attr.GetObject("title")
	var title string
	var language string
	titleObj.Visit(func(key []byte, v *fastjson.Value) {
		language = string(key)
		t, _ := v.StringBytes()
		title = string(t)
	})

	var desc string
	found := false
	attr.GetObject("description").Visit(func(key []byte, v *fastjson.Value) {
		if found {
			return
		}

		lang := string(key)

		d, _ := v.StringBytes()
		desc = string(d)

		if lang == language {
			found = true
		}
	})

	cover := ""
	rel := val.Get("data").GetArray("relationships")
	for _, v := range rel {
		relType := string(v.GetStringBytes("type"))
		if relType == "author" {
			authorId := string(v.GetStringBytes("id"))
			authorJSON, err := dexClient.RequestJSON(authorEndpoint, string(authorId))

			if err != nil {
				continue
			}

			author := string(authorJSON.Get("data").Get("attributes").GetStringBytes("name"))
			title = strings.Join([]string{title, " - ", author}, " ")
		}

		if relType == "cover_art" {
			coverId := string(v.GetStringBytes("id"))
			coverJSON, err := dexClient.RequestJSON(coverEndpoint, string(coverId))

			if err != nil {
				continue
			}

			filename := string(coverJSON.Get("data").Get("attributes").GetStringBytes("fileName"))
			cover = fmt.Sprintf(CoverUri, mangaId, filename)
		}

	}

	site := fmt.Sprintf("https://mangadex.org/title/%s", mangaId)
	return gin.H{
		"og_title":   title,
		"og_content": desc,
		"og_name":    site,
		"og_image":   cover,
		"redirect":   site,
	}
}

func createEmbed(c *gin.Context) {
	mangaId := c.Param("md-id")

	comicJSON, err := dexClient.RequestJSON(mangaEndpoint, mangaId)
	comicMeta := parseMangaResponse(comicJSON, mangaId)

	var status int
	if err != nil {
		fmt.Fprintf(gin.DefaultWriter, "[ERROR]: %v", err)
		status = http.StatusBadRequest
	} else {
		status = http.StatusOK
	}

	c.HTML(status, "embed.html", comicMeta)
}
