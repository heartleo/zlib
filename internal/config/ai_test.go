package config

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/heartleo/zlib/internal/aimatch"
)

func TestAISettingsRoundTripAndRemove(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	got, err := LoadAI()
	if err != nil || got.Configured() {
		t.Fatalf("missing file: %+v, %v", got, err)
	}
	s := aimatch.Settings{Protocol: aimatch.ProtocolAnthropic, BaseURL: "https://api.example.com", APIKey: strings.Repeat("k", 24), Model: "m", WebSearch: true}
	s.MarkVerified()
	if err := SaveAI(s); err != nil {
		t.Fatal(err)
	}
	got, err = LoadAI()
	if err != nil || !reflect.DeepEqual(got, s) || !got.Verified() {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	if info, err := os.Stat(AIPath()); err != nil || info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("saved file is readable by others: %v %v", info, err)
	}
	if removed, err := RemoveAI(); !removed || err != nil {
		t.Fatalf("remove: %v %v", removed, err)
	}
	if removed, _ := RemoveAI(); removed {
		t.Fatal("second remove reported a file")
	}
}
