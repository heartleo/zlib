package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"

	zlib "github.com/heartleo/zlib/internal/zlib"
)

const searchExtensionsEnv = "ZLIB_SEARCH_EXTENSIONS"

func parseDefaultSearchExtensions(value string) ([]zlib.Extension, error) {
	var extensions []zlib.Extension
	for _, raw := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' }) {
		ext := zlib.Extension(strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(raw), ".")))
		if !slices.Contains(searchFormats, ext) {
			return nil, fmt.Errorf("unsupported search format %q; use formats such as epub, pdf or azw3", raw)
		}
		if !slices.Contains(extensions, ext) {
			extensions = append(extensions, ext)
		}
	}
	return extensions, nil
}

func defaultSearchOptions() (*zlib.SearchOptions, error) {
	extensions, err := parseDefaultSearchExtensions(os.Getenv(searchExtensionsEnv))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", searchExtensionsEnv, err)
	}
	if len(extensions) == 0 {
		return nil, nil
	}
	return &zlib.SearchOptions{Extensions: extensions}, nil
}
