package cli

import (
	"context"

	zlib "github.com/heartleo/zlib/internal/zlib"
)

// domainCheckEvent is one finished domain probe, by index into the candidates.
type domainCheckEvent struct {
	index int
	check zlib.DomainCheck
}

// streamDomainChecks probes candidates concurrently and yields each result as
// soon as it finishes, then closes the channel. The buffer holds every result,
// so probes never block on a page that stopped reading (cancelled or closed).
func streamDomainChecks(ctx context.Context, candidates []string, proxy string) <-chan domainCheckEvent {
	ch := make(chan domainCheckEvent, len(candidates))
	go func() {
		defer close(ch)
		zlib.ProbeDomainsEach(ctx, candidates, zlib.DomainProbeOptions{ProxyURL: proxy, Path: "/eapi/info/domains"},
			func(i int, check zlib.DomainCheck) { ch <- domainCheckEvent{i, check} })
	}()
	return ch
}
