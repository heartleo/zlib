package cli

import "testing"

func TestHistoryPageFlagDefaultsToOne(t *testing.T) {
	flag := historyCmd.Flags().Lookup("page")
	if flag == nil {
		t.Fatal("expected page flag to be defined")
	}

	if flag.DefValue != "1" {
		t.Fatalf("expected default page to be 1, got %q", flag.DefValue)
	}

	page, err := historyCmd.Flags().GetInt("page")
	if err != nil {
		t.Fatalf("expected page flag to parse: %v", err)
	}
	if page != 1 {
		t.Fatalf("expected page flag value to default to 1, got %d", page)
	}
}
