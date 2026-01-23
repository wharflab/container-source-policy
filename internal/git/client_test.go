package git

import (
	"context"
	"testing"
)

func TestParseGitURL(t *testing.T) {
	tests := []struct {
		name       string
		rawURL     string
		wantRemote string
		wantRef    string
		wantSubdir string
		wantErr    bool
	}{
		{
			name:       "https with tag",
			rawURL:     "https://github.com/owner/repo.git#v1.0.0",
			wantRemote: "https://github.com/owner/repo.git",
			wantRef:    "v1.0.0",
			wantSubdir: "",
		},
		{
			name:       "https with branch",
			rawURL:     "https://github.com/owner/repo.git#main",
			wantRemote: "https://github.com/owner/repo.git",
			wantRef:    "main",
			wantSubdir: "",
		},
		{
			name:       "https with ref and subdir",
			rawURL:     "https://github.com/owner/repo.git#v1.0.0:subdirectory",
			wantRemote: "https://github.com/owner/repo.git",
			wantRef:    "v1.0.0",
			wantSubdir: "subdirectory",
		},
		{
			name:       "no fragment defaults to HEAD",
			rawURL:     "https://github.com/owner/repo.git",
			wantRemote: "https://github.com/owner/repo.git",
			wantRef:    "HEAD",
			wantSubdir: "",
		},
		{
			name:       "git@ SSH format",
			rawURL:     "git@github.com:owner/repo.git#branch",
			wantRemote: "git@github.com:owner/repo.git",
			wantRef:    "branch",
			wantSubdir: "",
		},
		{
			name:       "git protocol",
			rawURL:     "git://github.com/owner/repo#v2.0.0",
			wantRemote: "git://github.com/owner/repo",
			wantRef:    "v2.0.0",
			wantSubdir: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseGitURL(tt.rawURL)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseGitURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if got.Remote != tt.wantRemote {
				t.Errorf("ParseGitURL().Remote = %v, want %v", got.Remote, tt.wantRemote)
			}
			if got.Ref != tt.wantRef {
				t.Errorf("ParseGitURL().Ref = %v, want %v", got.Ref, tt.wantRef)
			}
			if got.Subdir != tt.wantSubdir {
				t.Errorf("ParseGitURL().Subdir = %v, want %v", got.Subdir, tt.wantSubdir)
			}
		})
	}
}

// TestGetCommitChecksum_Integration tests resolving a real git ref
// This is an integration test that requires network access
func TestGetCommitChecksum_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	client := NewClient()
	ctx := context.Background()

	// Test with a known stable tag from cli/cli
	checksum, err := client.GetCommitChecksum(ctx, "https://github.com/cli/cli.git#v2.40.0")
	if err != nil {
		t.Fatalf("GetCommitChecksum() error = %v", err)
	}

	// v2.40.0 tag should resolve to this specific commit
	want := "54d56cab3a0882b43ac794df59924dc3f93bb75c"
	if checksum != want {
		t.Errorf("GetCommitChecksum() = %v, want %v", checksum, want)
	}

	// Verify the SHA has correct length
	if len(checksum) != 40 {
		t.Errorf("GetCommitChecksum() returned SHA with length %d, want 40", len(checksum))
	}
}
