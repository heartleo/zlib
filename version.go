package zlib

import "runtime/debug"

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func init() {
	if Version != "dev" {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "(devel)" {
		Version = info.Main.Version
	}
}
