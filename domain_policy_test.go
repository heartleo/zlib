package zlib

import "testing"

func TestParseAllowedDomain(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "exact", value: "https://z-lib.gd/", want: "https://z-lib.gd"},
		{name: "subdomain", value: "https://api.z-lib.gl", want: "https://api.z-lib.gl"},
		{name: "case", value: "https://Z-LIB.SK", want: "https://z-lib.sk"},
		{name: "regional exception", value: "https://z-library.ec", want: "https://z-library.ec"},
		{name: "fake suffix", value: "https://evil-z-lib.sk", wantErr: true},
		{name: "suffix with trailing text", value: "https://z-lib.sk.evil.example", wantErr: true},
		{name: "removed sk domain", value: "https://z-library.sk", wantErr: true},
		{name: "HTTP", value: "http://z-lib.gd", wantErr: true},
		{name: "port", value: "https://z-lib.gd:8443", wantErr: true},
		{name: "path", value: "https://z-lib.gd/eapi", wantErr: true},
		{name: "userinfo", value: "https://user@z-lib.gd", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAllowedDomain(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseAllowedDomain(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseAllowedDomain(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestAllowedDomainSuffixesReturnsCopy(t *testing.T) {
	got := AllowedDomainSuffixes()
	got[0] = "modified.invalid"
	if AllowedDomainSuffixes()[0] != "z-library.gs" {
		t.Fatal("AllowedDomainSuffixes() exposed mutable internal storage")
	}
}
