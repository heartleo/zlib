package cli

import (
	"encoding/json"
	"os"
)

// printJSON writes v to stdout as a single JSON document with a trailing
// newline and no decoration, so `--json` output is directly consumable by
// jq, scripts, and the Claude Code plugin.
func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(v)
}
