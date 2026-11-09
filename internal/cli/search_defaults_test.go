package cli

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	zlib "github.com/heartleo/zlib/internal/zlib"
	"github.com/spf13/cobra"
)

func TestDefaultSearchFormatsSaveApplyAndClear(t *testing.T) {
	t.Setenv(searchExtensionsEnv, "")
	store := testSettingsStore(t)
	m, err := newSettingsModel(t.Context(), store, "search")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Any format") {
		t.Fatal("unset formats should allow any format")
	}
	if opts, err := defaultSearchOptions(); err != nil || opts != nil {
		t.Fatalf("unset defaults = %+v, %v", opts, err)
	}
	m.edit(searchExtensionsEnv)
	m, _ = settingsStep(t, m, key("space")) // EPUB
	m, _ = settingsStep(t, m, key("down"))
	m, _ = settingsStep(t, m, key("space")) // PDF
	m, _ = settingsStep(t, m, key("ctrl+s"))
	if m.page != "search" || os.Getenv(searchExtensionsEnv) != "epub,pdf" {
		t.Fatalf("formats not saved and applied: %s, %q", m.page, m.message)
	}
	stored, err := readSettingsEnv(store.global)
	if err != nil || stored[searchExtensionsEnv] != "epub,pdf" {
		t.Fatal("formats not persisted")
	}
	opts, err := defaultSearchOptions()
	if err != nil || !slices.Equal(opts.Extensions, []zlib.Extension{zlib.ExtEPUB, zlib.ExtPDF}) {
		t.Fatalf("defaults = %+v, %v", opts, err)
	}
	form := newSearchFilterForm("books", opts)
	if !slices.Equal(form.extensions, opts.Extensions) {
		t.Fatal("new search form lost the defaults")
	}
	// Clearing formats for this query must survive reopening its form.
	form.extensions = nil
	if refined := newSearchFilterForm("books", form.searchOptions()); len(refined.extensions) != 0 {
		t.Fatal("refine reapplied defaults after the user cleared them")
	}
	m.edit(searchExtensionsEnv)
	m, _ = settingsStep(t, m, key("x")) // No free-text entry.
	if m.page != "formats" || m.formatValue() != "epub,pdf" || os.Getenv(searchExtensionsEnv) != "epub,pdf" {
		t.Fatal("typing changed the format selection")
	}
	m, _ = settingsStep(t, m, key("ctrl+a")) // Select all, then clear the draft.
	m, _ = settingsStep(t, m, key("ctrl+a"))
	m, _ = settingsStep(t, m, key("enter"))
	if opts, err := defaultSearchOptions(); err != nil || opts != nil || m.value(searchExtensionsEnv).Value != "Any format" {
		t.Fatal("clearing default formats did not restore unrestricted search")
	}
	stored, err = readSettingsEnv(store.global)
	if err != nil || stored[searchExtensionsEnv] != "" {
		t.Fatal("cleared formats remained on disk")
	}
}

func TestSearchFlagsOverrideDefaultFormats(t *testing.T) {
	t.Setenv(searchExtensionsEnv, "epub,pdf")
	newCommand := func() *cobra.Command {
		cmd := &cobra.Command{}
		cmd.Flags().StringArray("ext", nil, "")
		cmd.Flags().StringArray("format", nil, "")
		return cmd
	}
	cmd := newCommand()
	opts, err := searchOptsFromFlags(cmd)
	if err != nil || !slices.Equal(opts.Extensions, []zlib.Extension{zlib.ExtEPUB, zlib.ExtPDF}) {
		t.Fatalf("default flag options = %+v, %v", opts, err)
	}
	for _, flag := range []string{"ext", "format"} {
		cmd := newCommand()
		if err := cmd.Flags().Set(flag, "azw3"); err != nil {
			t.Fatal(err)
		}
		opts, err := searchOptsFromFlags(cmd)
		if err != nil || !slices.Equal(opts.Extensions, []zlib.Extension{zlib.ExtAZW3}) {
			t.Fatalf("%s did not override defaults: %+v, %v", flag, opts, err)
		}
	}
	cmd = newCommand()
	if err := cmd.Flags().Set("ext", ""); err != nil {
		t.Fatal(err)
	}
	if opts, err := searchOptsFromFlags(cmd); err != nil || opts != nil {
		t.Fatalf("explicit empty extension did not clear defaults: %+v, %v", opts, err)
	}
}

func TestDefaultDownloadDirectorySettingAppliesImmediately(t *testing.T) {
	t.Setenv(downloadDirEnvVar, "")
	store := testSettingsStore(t)
	m, err := newSettingsModel(t.Context(), store, "download")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	m.edit(downloadDirEnvVar)
	m.input.SetValue(dir)
	m, _ = settingsStep(t, m, key("enter"))
	if got, err := defaultDownloadDirectory(); err != nil || got != dir {
		t.Fatalf("default directory = %q, %v", got, err)
	}
	m.edit(downloadDirEnvVar)
	m.input.SetValue("")
	m, _ = settingsStep(t, m, key("enter"))
	if got, err := defaultDownloadDirectory(); err != nil || got != "." {
		t.Fatalf("cleared directory = %q, %v", got, err)
	}
}
