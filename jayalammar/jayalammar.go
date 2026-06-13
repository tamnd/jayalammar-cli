// Package jayalammar is the library behind the jay command: the HTTP client,
// request shaping, and the typed data models for Jay Alammar's machine learning blog.
//
// Data comes from the public Atom feed at jalammar.github.io/feed.xml. No API
// key is required. The client sends a real User-Agent, paces requests, and
// retries 429/5xx with exponential backoff.
package jayalammar

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Config holds constructor parameters for Client.
type Config struct {
	BaseURL   string
	UserAgent string
	Rate      time.Duration
	Retries   int
	Timeout   time.Duration
}

// DefaultConfig returns sensible production defaults.
func DefaultConfig() Config {
	return Config{
		BaseURL:   "https://jalammar.github.io",
		UserAgent: "jay/dev (+https://github.com/tamnd/jayalammar-cli)",
		Rate:      500 * time.Millisecond,
		Retries:   3,
		Timeout:   30 * time.Second,
	}
}

// Client fetches the Jay Alammar Atom feed.
type Client struct {
	httpClient *http.Client
	baseURL    string
	userAgent  string
	rate       time.Duration
	retries    int
	mu         sync.Mutex
	last       time.Time
}

// NewClient returns a Client configured by cfg.
func NewClient(cfg Config) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: cfg.Timeout},
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		userAgent:  cfg.UserAgent,
		rate:       cfg.Rate,
		retries:    cfg.Retries,
	}
}

// Post is the record emitted for Jay Alammar blog posts.
type Post struct {
	Rank      int    `json:"rank"`
	Title     string `json:"title"`
	Published string `json:"published"`
	Summary   string `json:"summary"`
	URL       string `json:"url"`
}

// Latest fetches up to limit posts from the Atom feed ranked by feed order.
// limit=0 returns all entries.
func (c *Client) Latest(ctx context.Context, limit int) ([]Post, error) {
	rawURL := c.baseURL + "/feed.xml"
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	var feed atomFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("parse feed %s: %w", rawURL, err)
	}
	entries := feed.Entries
	if limit > 0 && limit < len(entries) {
		entries = entries[:limit]
	}
	out := make([]Post, len(entries))
	for i, e := range entries {
		out[i] = entryToPost(e, i+1)
	}
	return out, nil
}

// Search fetches the full feed and returns up to limit posts whose title or
// summary contains query (case-insensitive). limit=0 returns all matches.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]Post, error) {
	all, err := c.Latest(ctx, 0)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	var out []Post
	for _, p := range all {
		if strings.Contains(strings.ToLower(p.Title), q) ||
			strings.Contains(strings.ToLower(p.Summary), q) {
			out = append(out, p)
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// get fetches a URL with pacing and retries.
func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) ([]byte, bool, error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/atom+xml, application/xml, text/xml")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

func (c *Client) pace() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rate <= 0 {
		return
	}
	if wait := c.rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// ─── Atom wire types ─────────────────────────────────────────────────────────

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title     string      `xml:"title"`
	Link      atomLink    `xml:"link"`
	Published string      `xml:"published"`
	Summary   string      `xml:"summary"`
	Content   string      `xml:"content"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func entryToPost(e atomEntry, rank int) Post {
	summary := e.Summary
	if summary == "" {
		summary = e.Content
	}
	return Post{
		Rank:      rank,
		Title:     strings.TrimSpace(e.Title),
		Published: parseDate(e.Published),
		Summary:   stripAndTruncate(summary, 150),
		URL:       strings.TrimSpace(e.Link.Href),
	}
}

// parseDate parses an Atom/RFC3339 date and returns "2006-01-02". Falls back
// to the raw string on parse error.
func parseDate(s string) string {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z07:00", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format("2006-01-02")
		}
	}
	return s
}

// stripAndTruncate strips HTML tags, decodes common entities, and truncates
// to maxChars runes, appending "…" if truncated.
func stripAndTruncate(html string, maxChars int) string {
	var b strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	out := b.String()
	out = strings.ReplaceAll(out, "&amp;", "&")
	out = strings.ReplaceAll(out, "&lt;", "<")
	out = strings.ReplaceAll(out, "&gt;", ">")
	out = strings.ReplaceAll(out, "&quot;", `"`)
	out = strings.ReplaceAll(out, "&#39;", "'")
	out = strings.ReplaceAll(out, "&apos;", "'")
	out = strings.TrimSpace(out)
	rs := []rune(out)
	if len(rs) > maxChars {
		return string(rs[:maxChars-1]) + "…"
	}
	return out
}
