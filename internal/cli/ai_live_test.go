package cli

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/heartleo/zlib/internal/aimatch"
	"github.com/heartleo/zlib/internal/metasource"
	"github.com/joho/godotenv"
)

// TestLiveAIEndpoints checks the configured AI endpoints with the real client.
// It is opt-in: it spends a few tokens and needs the network. Run it with
//
//	ZLIB_AI_LIVE=1 go test -run TestLiveAIEndpoints -v ./internal/cli
//
// It reads OPENAI_BASE_URL, OPENAI_API_KEY, OPENAI_MODEL and the ANTHROPIC_
// equivalents, and ZLIB_PROXY, from the repository's .env (or the file named by
// ZLIB_AI_LIVE_ENV). It logs hosts, counts and redacted errors, never a key.
func TestLiveAIEndpoints(t *testing.T) {
	if os.Getenv("ZLIB_AI_LIVE") != "1" {
		t.Skip("set ZLIB_AI_LIVE=1 to call the configured AI endpoints")
	}
	// Only the file is read: the process environment may hold the variables of
	// other tools (ANTHROPIC_API_KEY, ANTHROPIC_BASE_URL) that are not ours.
	path := os.Getenv("ZLIB_AI_LIVE_ENV")
	if path == "" {
		path = "../../.env"
	}
	env, err := godotenv.Read(path)
	if err != nil {
		t.Fatalf("cannot read the env file: %v", err)
	}
	get := func(k string) string { return env[k] }
	if proxy := get("ZLIB_PROXY"); proxy != "" {
		t.Setenv("ZLIB_PROXY", proxy)
	}
	for _, tc := range []struct {
		name  string
		proto aimatch.Protocol
		prefx string
	}{{"openai", aimatch.ProtocolOpenAI, "OPENAI_"}, {"anthropic", aimatch.ProtocolAnthropic, "ANTHROPIC_"}} {
		t.Run(tc.name, func(t *testing.T) {
			s := aimatch.Settings{Protocol: tc.proto, BaseURL: get(tc.prefx + "BASE_URL"), APIKey: get(tc.prefx + "API_KEY"), Model: get(tc.prefx + "MODEL")}
			if s.BaseURL == "" || s.APIKey == "" || s.Model == "" {
				t.Skip("not configured in .env")
			}
			u, _ := url.Parse(s.BaseURL)
			// The model is not logged: a misplaced key in .env would show up in it.
			t.Logf("endpoint host=%s model(len)=%d key=%s", u.Host, len(s.Model), s.MaskedKey())
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
			defer cancel()

			lister, err := newAILister(s)
			if err != nil {
				t.Fatalf("settings rejected: %v", err)
			}
			if ids, err := lister.Models(ctx); err != nil {
				t.Logf("model list: %v (not every gateway has one)", err)
			} else {
				t.Logf("model list: %d models", len(ids))
			}

			client, err := newAIClient(s)
			if err != nil {
				t.Fatalf("settings rejected: %v", err)
			}
			if err := client.Test(ctx); err != nil {
				t.Fatalf("connection test failed: %v", err)
			}
			t.Log("connection test: ok")

			// The match itself: one catalogue row, ranked by the real model.
			row := metasource.Candidate{Sources: []string{"openlibrary"}, Title: "The Trial", Authors: []string{"Franz Kafka"}, EditionLevel: true}
			lookup := func(context.Context, metasource.Query) (metasource.Result, error) { return metasource.Result{}, nil }
			out, err := aimatch.Match(ctx, client, lookup, aimatch.Input{
				FileName: "book.epub", Query: metasource.Query{Title: "Der Process", Authors: []string{"Franz Kafka"}}, Existing: []metasource.Candidate{row},
			}, func(step string) { t.Logf("step: %s", step) })
			if err != nil {
				t.Fatalf("match failed: %v", err)
			}
			t.Logf("match: %d recommendation(s)", len(out.Recommendations))
			for _, r := range out.Recommendations {
				if r.Candidate.Title != row.Title {
					t.Errorf("a recommendation is not the catalogue row: %q", r.Candidate.Title)
				}
				t.Logf("  %s %.0f%% %s", r.Relation, r.Confidence*100, r.Explanation)
			}
		})
	}
}
