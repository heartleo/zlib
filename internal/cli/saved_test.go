package cli

import (
	"encoding/json"
	"testing"

	zlib "github.com/heartleo/zlib/internal/zlib"
)

// favorites, recommended and most-popular --json used to print the bare
// numeric ID, which `zlib download` rejects; every JSON book list must carry
// the id:hash reference.
func TestBookListJSONCarriesDownloadableIDs(t *testing.T) {
	data, err := json.Marshal(downloadableBooks([]zlib.Book{
		{ID: "123", Hash: "abc"},
		{ID: "456"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	var got []struct{ ID string }
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got[0].ID != "123:abc" || got[1].ID != "456" {
		t.Errorf("ids = %q and %q, want 123:abc and 456 (no hash, nothing to add): %s", got[0].ID, got[1].ID, data)
	}
	// Agents treat non-array output as a failure, so an empty page must not
	// encode as null.
	if data, _ := json.Marshal(downloadableBooks(nil)); string(data) != "[]" {
		t.Fatalf("empty book list encodes as %s, want []", data)
	}
}

func TestFavoritesIsOnlyCollectionCommand(t *testing.T) {
	if _, _, err := rootCmd.Find([]string{"saved"}); err == nil {
		t.Fatal("saved command should not be registered")
	}
	favorites, _, err := rootCmd.Find([]string{"favorites"})
	if err != nil {
		t.Fatal(err)
	}
	if favorites.Name() != "favorites" {
		t.Fatalf("command = %v", favorites)
	}
	command := favorites
	for _, flag := range []string{"page", "json", "dir", "send-to-kindle"} {
		if command.Flags().Lookup(flag) == nil {
			t.Errorf("%s is missing --%s", command.Name(), flag)
		}
	}
}
