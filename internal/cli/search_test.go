package cli

import "testing"

func TestSearchPageFlagDefaultsToOne(t *testing.T) {
	flag := searchCmd.Flags().Lookup("page")
	if flag == nil {
		t.Fatal("expected page flag to be defined")
	}

	if flag.DefValue != "1" {
		t.Fatalf("expected default page to be 1, got %q", flag.DefValue)
	}

	page, err := searchCmd.Flags().GetInt("page")
	if err != nil {
		t.Fatalf("expected page flag to parse: %v", err)
	}
	if page != 1 {
		t.Fatalf("expected page flag value to default to 1, got %d", page)
	}
}

func TestSearchCountFlagDefaultsToFifty(t *testing.T) {
	flag := searchCmd.Flags().Lookup("count")
	if flag == nil {
		t.Fatal("expected count flag to be defined")
	}

	if flag.DefValue != "50" {
		t.Fatalf("expected default count to be 50, got %q", flag.DefValue)
	}

	count, err := searchCmd.Flags().GetInt("count")
	if err != nil {
		t.Fatalf("expected count flag to parse: %v", err)
	}
	if count != 50 {
		t.Fatalf("expected count flag value to default to 50, got %d", count)
	}
}
