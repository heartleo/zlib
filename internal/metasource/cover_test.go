package metasource

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

func testImage(t *testing.T, w, h int, format string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	var buf bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func coverClient(srv string) *http.Client {
	u, _ := url.Parse(srv)
	return newHTTPClient(nil, 0, hostPolicy{u.Host}, u.Scheme)
}

func TestFetchCoverAcceptsARealImage(t *testing.T) {
	jpg := testImage(t, 200, 300, "jpeg")
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || !strings.HasPrefix(r.Header.Get("User-Agent"), "zlib-metadata-lookup") {
			t.Errorf("headers = %v", r.Header)
		}
		w.Write(jpg)
	})
	cov, err := fetchCover(context.Background(), coverClient(srv.URL), srv.URL+"/c.jpg")
	if err != nil || cov.MediaType != "image/jpeg" || !bytes.Equal(cov.Data, jpg) || cov.Ext() != ".jpg" {
		t.Fatalf("cov = %+v, err = %v", cov, err)
	}
}

func TestFetchCoverRejectsBadDownloads(t *testing.T) {
	cases := map[string]struct {
		handler http.HandlerFunc
		want    string
	}{
		"html error page": {func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>nope</html>")) }, "not a JPEG, PNG, GIF or WebP"},
		"not found":       {func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }, "no cover for this book"},
		"rate limited":    {func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429) }, "rate limited"},
		"server error":    {func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }, "HTTP status 500"},
		"thumbnail":       {func(w http.ResponseWriter, r *http.Request) { w.Write(testImage(t, 30, 40, "png")) }, "too small (30x40)"},
		"truncated image": {func(w http.ResponseWriter, r *http.Request) { w.Write(testImage(t, 200, 300, "png")[:20]) }, "unreadable image"},
		"huge": {func(w http.ResponseWriter, r *http.Request) {
			w.Write(append([]byte("\xff\xd8\xff"), make([]byte, maxCoverBytes)...))
		}, "too large"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := serve(t, tc.handler)
			_, err := fetchCover(context.Background(), coverClient(srv.URL), srv.URL+"/c")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestFetchCoverOnlyFromTrustedHosts(t *testing.T) {
	for _, raw := range []string{
		"http://covers.openlibrary.org/b/id/1-L.jpg", // not https
		"https://evil.example/b/id/1-L.jpg",          // other host
		"https://covers.openlibrary.org:8443/x.jpg",  // other port
		"https://openlibrary.org.evil.example/x.jpg", // look-alike
		"https://notarchive.org/x.jpg",               // suffix without the dot
		"file:///etc/passwd",
		"https://user:pw@evil.example/x.jpg",
		"::not a url",
	} {
		if _, err := FetchCover(context.Background(), nil, raw); err == nil || !strings.Contains(err.Error(), "trusted host") {
			t.Errorf("%s: err = %v", raw, err)
		}
	}
	for _, raw := range []string{
		"https://covers.openlibrary.org/b/id/1-L.jpg",
		"https://ia800000.us.archive.org/x.jpg",
		"https://archive.org/x.jpg",
		"https://books.google.com/books/content?id=abc",
	} {
		u, _ := url.Parse(raw)
		if !coverHosts.allows(u) {
			t.Errorf("%s should be allowed", raw)
		}
	}
}

func TestFetchCoverRefusesOffHostRedirects(t *testing.T) {
	other := serve(t, func(w http.ResponseWriter, r *http.Request) { t.Error("followed an off-host redirect") })
	redirect := serve(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) })
	if _, err := fetchCover(context.Background(), coverClient(redirect.URL), redirect.URL+"/c"); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("err = %v", err)
	}
}

func TestPlanOffersTheCoverAsAFillOnly(t *testing.T) {
	c := Candidate{Title: "Dune", CoverURL: "https://covers.openlibrary.org/b/id/7-L.jpg?default=false"}
	plan := Plan(&ebookmeta.Metadata{Title: "Dune"}, c)
	if len(plan) != 1 || plan[0].Field != FieldCover || !plan[0].DefaultSelected() || plan[0].CoverURL() != c.CoverURL ||
		plan[0].New != "image from covers.openlibrary.org" {
		t.Fatalf("plan = %+v", plan)
	}
	replace := Plan(&ebookmeta.Metadata{Title: "Dune", Cover: "image/png"}, c)
	if len(replace) != 1 || replace[0].DefaultSelected() || replace[0].Old != "image/png" {
		t.Fatalf("plan = %+v", replace)
	}
	if BuildChanges(plan, Change.DefaultSelected) != nil {
		t.Error("BuildChanges invented a cover without its bytes")
	}
}
