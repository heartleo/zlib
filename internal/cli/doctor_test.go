package cli

import "testing"

func TestValidateDoctorProxy(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "unset", value: "", want: ""},
		{name: "HTTP", value: "http://127.0.0.1:7890", want: "http://127.0.0.1:7890"},
		{name: "SOCKS5", value: "socks5://127.0.0.1:9050", want: "socks5://127.0.0.1:9050"},
		{name: "SOCKS5 hostname", value: "socks5h://127.0.0.1:9050", want: "socks5h://127.0.0.1:9050"},
		{name: "missing scheme", value: "127.0.0.1:7890", wantErr: true},
		{name: "unsupported scheme", value: "ftp://127.0.0.1:21", wantErr: true},
		{name: "path", value: "http://127.0.0.1:7890/proxy", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateDoctorProxy(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateDoctorProxy(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("validateDoctorProxy(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
