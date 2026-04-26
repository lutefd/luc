package extensions

import (
	"reflect"
	"testing"
)

func TestGitRemoteCandidates(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "https github prefers ssh then falls back to https",
			in:   "https://github.com/acme/widget",
			want: []string{"git@github.com:acme/widget.git", "https://github.com/acme/widget"},
		},
		{
			name: "https with .git suffix preserved in fallback",
			in:   "https://gitlab.example.com/group/repo.git",
			want: []string{"git@gitlab.example.com:group/repo.git", "https://gitlab.example.com/group/repo.git"},
		},
		{
			name: "http converts to ssh with http fallback",
			in:   "http://git.internal/acme/widget",
			want: []string{"git@git.internal:acme/widget.git", "http://git.internal/acme/widget"},
		},
		{
			name: "scp-like ssh falls back to https",
			in:   "git@github.com:acme/widget.git",
			want: []string{"git@github.com:acme/widget.git", "https://github.com/acme/widget.git"},
		},
		{
			name: "ssh scheme falls back to https",
			in:   "ssh://git@github.com/acme/widget.git",
			want: []string{"ssh://git@github.com/acme/widget.git", "https://github.com/acme/widget.git"},
		},
		{
			name: "git scheme passes through",
			in:   "git://example.com/acme/widget.git",
			want: []string{"git://example.com/acme/widget.git"},
		},
		{
			name: "file scheme passes through",
			in:   "file:///tmp/repo.git",
			want: []string{"file:///tmp/repo.git"},
		},
		{
			name: "empty input returns nil",
			in:   "",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gitRemoteCandidates(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("gitRemoteCandidates(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}

func TestHTTPSToSSH(t *testing.T) {
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"https://github.com/acme/widget", "git@github.com:acme/widget.git", true},
		{"https://github.com/acme/widget.git", "git@github.com:acme/widget.git", true},
		{"https://user:token@github.com/acme/widget", "git@github.com:acme/widget.git", true},
		{"https://github.com:443/acme/widget", "git@github.com:acme/widget.git", true},
		{"https://github.com/", "", false},
		{"https://github.com", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := httpsToSSH(tt.in)
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("httpsToSSH(%q) = (%q,%v), want (%q,%v)", tt.in, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestSSHToHTTPS(t *testing.T) {
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"git@github.com:acme/widget.git", "https://github.com/acme/widget.git", true},
		{"ssh://git@github.com/acme/widget.git", "https://github.com/acme/widget.git", true},
		{"git@github.com:", "", false},
		{"not-an-ssh-url", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := sshToHTTPS(tt.in)
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("sshToHTTPS(%q) = (%q,%v), want (%q,%v)", tt.in, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestIsScpLikeGitURL(t *testing.T) {
	cases := map[string]bool{
		"git@github.com:acme/widget.git":   true,
		"user@host.example:path/to/repo":   true,
		"https://github.com/acme/widget":   false,
		"ssh://git@github.com/acme/widget": false,
		"C:\\Users\\dev\\repo":             false,
		"/tmp/repo":                        false,
		"":                                 false,
	}
	for in, want := range cases {
		if got := isScpLikeGitURL(in); got != want {
			t.Errorf("isScpLikeGitURL(%q) = %v, want %v", in, got, want)
		}
	}
}
