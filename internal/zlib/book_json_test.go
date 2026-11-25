package zlib

import (
	"encoding/json"
	"testing"
)

// Every JSON document a command prints carries the reference `zlib download`
// accepts. favorites, recommended, most-popular and history each shipped the bare
// numeric ID once, because the rewrite lived in the caller.
func TestBookJSONCarriesTheDownloadableReference(t *testing.T) {
	for _, tc := range []struct{ id, hash, want string }{
		{"123", "abc", "123:abc"},
		{"123:abc", "abc", "123:abc"},
		{"456", "", "456"},
	} {
		data, err := json.Marshal(Book{ID: tc.id, Hash: tc.hash})
		if err != nil {
			t.Fatal(err)
		}
		var got struct{ ID string }
		if err := json.Unmarshal(data, &got); err != nil || got.ID != tc.want {
			t.Errorf("id %q hash %q marshals as %s, want id %q", tc.id, tc.hash, data, tc.want)
		}
	}
	data, _ := json.Marshal(SearchResult{Books: []Book{{ID: "1", Hash: "h"}}})
	var res struct{ Books []struct{ ID string } }
	if err := json.Unmarshal(data, &res); err != nil || res.Books[0].ID != "1:h" {
		t.Errorf("a book inside a result marshals as %s", data)
	}
}
