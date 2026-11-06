package weread

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestUserInfoReadsNicknameAndKeys(t *testing.T) {
	f := newFake(t)
	f.mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if r.URL.Query().Get("userVid") != "1001" {
			t.Errorf("userVid = %q", r.URL.Query().Get("userVid"))
		}
		writeJSON(w, map[string]any{"userVid": 1001, "name": "Reader\x1b[31m\nOne", "avatar": "https://example.com/a.png", "gender": 1})
	})
	var saved []Credentials
	c := f.client(WithCredentials(testCreds()), WithCredentialsSaver(func(creds Credentials) error {
		saved = append(saved, creds)
		return nil
	}))
	info, err := c.UserInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "Reader[31m One" {
		t.Fatalf("name = %q, want cleaned single-line text", info.Name)
	}
	if strings.Join(info.Keys, ",") != "avatar,gender,name,userVid" {
		t.Fatalf("keys = %v", info.Keys)
	}

	name, err := c.UpdateName(context.Background())
	if err != nil || name != info.Name || c.Credentials().Name != info.Name || len(saved) != 1 {
		t.Fatalf("UpdateName = %q, %v; saved %d times", name, err, len(saved))
	}
	if _, err := c.UpdateName(context.Background()); err != nil || len(saved) != 1 {
		t.Fatal("an unchanged name was saved again")
	}
}

func TestUserInfoFindsNestedNameAndToleratesMissingName(t *testing.T) {
	f := newFake(t)
	body := map[string]any{"user": map[string]any{"name": "Nested"}}
	f.mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, body) })
	c := f.client(WithCredentials(testCreds()))
	info, err := c.UserInfo(context.Background())
	if err != nil || info.Name != "Nested" || strings.Join(info.Keys, ",") != "user,user.name" {
		t.Fatalf("info = %+v, %v", info, err)
	}

	body = map[string]any{"userVid": 1001}
	creds := testCreds()
	creds.Name = "Kept"
	c = f.client(WithCredentials(creds))
	if name, err := c.UpdateName(context.Background()); err != nil || name != "Kept" {
		t.Fatalf("UpdateName without a name = %q, %v; want the saved name kept", name, err)
	}
}

func TestUserInfoRequiresLogin(t *testing.T) {
	if _, err := NewClient().UserInfo(context.Background()); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("err = %v", err)
	}
}
