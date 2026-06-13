package jayalammar_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/jayalammar-cli/jayalammar"
)

// atomXML wraps entries in a minimal valid Atom feed.
func atomXML(entries string) string {
	return `<?xml version="1.0" encoding="utf-8"?>` +
		`<feed xmlns="http://www.w3.org/2005/Atom">` +
		`<title>Jay Alammar</title>` +
		entries +
		`</feed>`
}

func singleEntry(title, href, published, summary string) string {
	return `<entry>` +
		`<title type="html">` + title + `</title>` +
		`<link href="` + href + `" rel="alternate" type="text/html" />` +
		`<published>` + published + `</published>` +
		`<summary type="html"><![CDATA[` + summary + `]]></summary>` +
		`</entry>`
}

func newTestClient(ts *httptest.Server) *jayalammar.Client {
	cfg := jayalammar.DefaultConfig()
	cfg.BaseURL = ts.URL
	cfg.Rate = 0
	return jayalammar.NewClient(cfg)
}

func TestLatestParsesTitle(t *testing.T) {
	feed := atomXML(singleEntry(
		"Illustrated Transformer",
		"https://jalammar.github.io/illustrated-transformer/",
		"2018-06-27T00:00:00+00:00",
		"A visual guide to transformers.",
	))
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(feed))
	}))
	defer ts.Close()

	posts, err := newTestClient(ts).Latest(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 {
		t.Fatalf("got %d posts, want 1", len(posts))
	}
	if posts[0].Title != "Illustrated Transformer" {
		t.Errorf("Title = %q", posts[0].Title)
	}
}

func TestLatestParsesURL(t *testing.T) {
	wantURL := "https://jalammar.github.io/illustrated-transformer/"
	feed := atomXML(singleEntry(
		"Illustrated Transformer",
		wantURL,
		"2018-06-27T00:00:00+00:00",
		"A visual guide.",
	))
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(feed))
	}))
	defer ts.Close()

	posts, err := newTestClient(ts).Latest(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if posts[0].URL != wantURL {
		t.Errorf("URL = %q, want %q", posts[0].URL, wantURL)
	}
}

func TestLatestParsesDate(t *testing.T) {
	feed := atomXML(singleEntry(
		"BERT Explained",
		"https://jalammar.github.io/illustrated-bert/",
		"2019-12-03T00:00:00+00:00",
		"BERT overview.",
	))
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(feed))
	}))
	defer ts.Close()

	posts, err := newTestClient(ts).Latest(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if posts[0].Published != "2019-12-03" {
		t.Errorf("Published = %q, want %q", posts[0].Published, "2019-12-03")
	}
}

func TestLatestStripsSummaryHTML(t *testing.T) {
	feed := atomXML(singleEntry(
		"HTML Stripping",
		"https://jalammar.github.io/test/",
		"2020-01-01T00:00:00+00:00",
		"<p>This is the <b>summary</b> text.</p>",
	))
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(feed))
	}))
	defer ts.Close()

	posts, err := newTestClient(ts).Latest(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(posts[0].Summary, "<") || strings.Contains(posts[0].Summary, ">") {
		t.Errorf("Summary contains HTML tags: %q", posts[0].Summary)
	}
	if !strings.Contains(posts[0].Summary, "summary") {
		t.Errorf("Summary text missing: %q", posts[0].Summary)
	}
}

func TestLatestTruncatesSummary(t *testing.T) {
	long := strings.Repeat("x", 300)
	feed := atomXML(singleEntry(
		"Long Post",
		"https://jalammar.github.io/long/",
		"2020-01-01T00:00:00+00:00",
		"<p>"+long+"</p>",
	))
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(feed))
	}))
	defer ts.Close()

	posts, err := newTestClient(ts).Latest(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	runes := []rune(posts[0].Summary)
	if len(runes) > 150 {
		t.Errorf("Summary too long: %d runes", len(runes))
	}
	if !strings.HasSuffix(posts[0].Summary, "…") {
		t.Errorf("Summary missing ellipsis: %q", posts[0].Summary)
	}
}

func TestLatestRankOrder(t *testing.T) {
	entries := singleEntry("A", "https://jalammar.github.io/a/", "2018-01-01T00:00:00+00:00", "") +
		singleEntry("B", "https://jalammar.github.io/b/", "2019-01-01T00:00:00+00:00", "") +
		singleEntry("C", "https://jalammar.github.io/c/", "2020-01-01T00:00:00+00:00", "")
	feed := atomXML(entries)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(feed))
	}))
	defer ts.Close()

	posts, err := newTestClient(ts).Latest(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 3 {
		t.Fatalf("got %d posts, want 3", len(posts))
	}
	for i, p := range posts {
		if p.Rank != i+1 {
			t.Errorf("posts[%d].Rank = %d, want %d", i, p.Rank, i+1)
		}
	}
}

func TestLatestLimit(t *testing.T) {
	entries := ""
	for i := 0; i < 5; i++ {
		entries += singleEntry("T", "https://jalammar.github.io/t/", "2020-01-01T00:00:00+00:00", "")
	}
	feed := atomXML(entries)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(feed))
	}))
	defer ts.Close()

	posts, err := newTestClient(ts).Latest(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 2 {
		t.Errorf("got %d posts with limit=2, want 2", len(posts))
	}
}

func TestSearchFiltersByTitle(t *testing.T) {
	entries := singleEntry("Illustrated Transformer", "https://jalammar.github.io/transformer/", "2018-01-01T00:00:00+00:00", "About transformers.") +
		singleEntry("GPT-2 Language Model", "https://jalammar.github.io/gpt2/", "2019-01-01T00:00:00+00:00", "About language models.")
	feed := atomXML(entries)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(feed))
	}))
	defer ts.Close()

	posts, err := newTestClient(ts).Search(context.Background(), "transformer", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 {
		t.Fatalf("got %d posts, want 1", len(posts))
	}
	if posts[0].Title != "Illustrated Transformer" {
		t.Errorf("Title = %q", posts[0].Title)
	}
}

func TestSearchFiltersBySummary(t *testing.T) {
	entries := singleEntry("Random Post", "https://jalammar.github.io/rand/", "2020-01-01T00:00:00+00:00", "This talks about attention mechanism.") +
		singleEntry("Another Post", "https://jalammar.github.io/other/", "2020-01-02T00:00:00+00:00", "Nothing relevant here.")
	feed := atomXML(entries)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(feed))
	}))
	defer ts.Close()

	posts, err := newTestClient(ts).Search(context.Background(), "attention", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 {
		t.Fatalf("got %d posts, want 1", len(posts))
	}
}

func TestSearchReturnsEmpty(t *testing.T) {
	feed := atomXML(singleEntry("Illustrated Transformer", "https://jalammar.github.io/transformer/", "2018-01-01T00:00:00+00:00", "About transformers."))
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(feed))
	}))
	defer ts.Close()

	posts, err := newTestClient(ts).Search(context.Background(), "zyxwvutsrq", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 0 {
		t.Errorf("got %d posts, want 0", len(posts))
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	feed := atomXML(singleEntry("Test", "https://jalammar.github.io/test/", "2020-01-01T00:00:00+00:00", ""))
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(feed))
	}))
	defer ts.Close()

	cfg := jayalammar.DefaultConfig()
	cfg.BaseURL = ts.URL
	cfg.Rate = 0
	cfg.Retries = 5
	c := jayalammar.NewClient(cfg)

	start := time.Now()
	_, err := c.Latest(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestGetUserAgent(t *testing.T) {
	var gotUA string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(atomXML("")))
	}))
	defer ts.Close()

	cfg := jayalammar.DefaultConfig()
	cfg.BaseURL = ts.URL
	cfg.Rate = 0
	c := jayalammar.NewClient(cfg)
	_, _ = c.Latest(context.Background(), 0)

	if gotUA == "" {
		t.Error("request carried no User-Agent")
	}
}
