package zlib

import "runtime/debug"

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// init fills version metadata from the embedded build info when it was not
// injected via ldflags (e.g. when installed with `go install ...@latest`).
func init() {
	if Version != "dev" {
		return
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = info.Main.Version
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if s.Value != "" {
				Commit = s.Value
			}
		case "vcs.time":
			if s.Value != "" {
				Date = s.Value
			}
		}
	}
}
