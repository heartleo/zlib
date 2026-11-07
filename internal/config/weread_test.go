package config

import (
	"os"
	"testing"

	"github.com/heartleo/zlib/internal/weread"
)

func TestWeReadSaverRejectsStaleLogin(t *testing.T) {
	for _, logout := range []bool{true, false} {
		t.Run(map[bool]string{true: "logout", false: "replacement"}[logout], func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			old := weread.Credentials{VID: "1", AccessToken: "fixture", RefreshToken: "fixture", DeviceID: "fixture"}
			if err := SaveWeRead(old); err != nil {
				t.Fatal(err)
			}
			save := WeReadSaver(old)
			next := old
			next.Name = "Reader"
			if logout {
				if _, err := RemoveWeRead(); err != nil {
					t.Fatal(err)
				}
			} else {
				replacement := old
				replacement.VID = "2"
				if err := SaveWeRead(replacement); err != nil {
					t.Fatal(err)
				}
			}
			if err := save(next); err == nil {
				t.Fatal("stale save succeeded")
			}
			if logout {
				if _, err := os.Stat(WeReadPath()); !os.IsNotExist(err) {
					t.Fatal("login recreated")
				}
			} else {
				got, err := LoadWeRead()
				if err != nil || got.VID != "2" {
					t.Fatal("replacement overwritten")
				}
			}
		})
	}
}
