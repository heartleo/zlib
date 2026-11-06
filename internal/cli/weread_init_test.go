package cli

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/heartleo/zlib/internal/weread"
)

// blockingTransport counts requests and holds each until its context ends,
// like a slow server, so a page's goroutine stays alive for the test.
type blockingTransport struct{ requests atomic.Int32 }

func (b *blockingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	b.requests.Add(1)
	<-r.Context().Done()
	return nil, r.Context().Err()
}

func countWeReadRequests(t *testing.T) *blockingTransport {
	t.Helper()
	transport := &blockingTransport{}
	prev := newWeReadClient
	t.Cleanup(func() { newWeReadClient = prev })
	newWeReadClient = func(opts ...weread.Option) *weread.Client {
		return weread.NewClient(append(opts, weread.WithHTTPClient(&http.Client{Transport: transport}))...)
	}
	return transport
}

// settle waits for the first request, then long enough for a second
// goroutine to make its own, and returns how many were made.
func settle(transport *blockingTransport) int32 {
	deadline := time.Now().Add(2 * time.Second)
	for transport.requests.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	return transport.requests.Load()
}

// drain waits for the page's goroutine to finish and close its channel.
func drain(t *testing.T, ch <-chan tea.Msg) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case _, open := <-ch:
			if !open {
				return
			}
		case <-timeout:
			t.Fatal("the page's goroutine did not stop after cancel")
		}
	}
}

// The program calls Init on the current page when it starts, and a standalone
// command has already opened the page once. Starting the flow twice put two
// goroutines on one channel, which closed it twice and panicked on cancel.
func TestWeReadLoginInitStartsOneFlow(t *testing.T) {
	transport := countWeReadRequests(t)
	m := newWeReadLoginModel(newWeReadClient())
	m.Init()
	m.Init()
	if got := settle(transport); got != 1 {
		t.Fatalf("two Init calls made %d QR requests, want 1", got)
	}
	m.cancel()
	drain(t, m.ch)
}

func TestWeReadImportInitStartsOneImport(t *testing.T) {
	transport := countWeReadRequests(t)
	creds := weread.Credentials{VID: "1001", AccessToken: "a", RefreshToken: "r", DeviceID: "d"}
	m := newWeReadImportModel(writeTempFile(t, "Dune.epub"), newWeReadClient(weread.WithCredentials(creds)))
	m.Init()
	m.Init()
	if got := settle(transport); got != 1 {
		t.Fatalf("two Init calls made %d upload requests, want 1; a second one spends another import", got)
	}
	m.cancel()
	drain(t, m.ch)
}

// zlib weread login opens the page before the program starts, then the
// program initializes it again. Cancelling must neither panic nor leave a
// second flow running.
func TestStandaloneWeReadLoginStartsOneFlowAndCancelsCleanly(t *testing.T) {
	isolateConfigHome(t)
	transport := countWeReadRequests(t)
	m, err := newStandaloneShellCommand("/weread login")
	if err != nil {
		t.Fatal(err)
	}
	m.Init() // what tea.NewProgram does first
	if got := settle(transport); got != 1 {
		t.Fatalf("standalone login made %d QR requests, want 1", got)
	}
	page, ok := m.page.(wereadLoginModel)
	if !ok {
		t.Fatalf("page = %T, want the login page", m.page)
	}
	next, _ := page.Update(key("esc"))
	drain(t, next.(wereadLoginModel).ch)
}

// A new code after expiry starts its own flow on its own channel, and the
// old flow's channel closing must not reach it.
func TestWeReadLoginRestartDoesNotShareChannel(t *testing.T) {
	transport := countWeReadRequests(t)
	m := newWeReadLoginModel(newWeReadClient())
	m.Init()
	settle(transport)
	oldCh := m.ch
	m, _ = m.restart()
	if m.ch == oldCh {
		t.Fatal("restart reused the old channel")
	}
	drain(t, oldCh)
	m.cancel()
	drain(t, m.ch)
}
