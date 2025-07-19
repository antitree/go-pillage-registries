package pillage

import (
	_ "embed"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
)

func setupTestRegistry(t *testing.T) (host, repo, tag string, cleanup func()) {
	t.Helper()
	srv := httptest.NewServer(registry.New())
	host = strings.TrimPrefix(srv.URL, "http://")
	repo = "test/repo"
	tag = "tag"
	img, err := random.Image(1024, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := crane.Push(img, fmt.Sprintf("%s/%s:%s", host, repo, tag)); err != nil {
		t.Fatal(err)
	}
	cleanup = srv.Close
	return
}

func TestMakeCraneOptions(t *testing.T) {
	type args struct {
		insecure bool
	}
	tests := []struct {
		name string
		args args
		want int
	}{
		{name: "secure options", args: args{insecure: false}, want: 1},
		{name: "insecure options", args: args{insecure: true}, want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotOptions := MakeCraneOptions(tt.args.insecure, nil); !reflect.DeepEqual(len(gotOptions), tt.want) {
				t.Errorf("MakeCraneOptions() length = %v, want %v", len(gotOptions), tt.want)
			}
		})
	}
}

func Test_securejoin(t *testing.T) {
	type args struct {
		paths []string
	}
	tests := []struct {
		name    string
		args    args
		wantOut string
	}{
		{
			name:    "basic join",
			args:    args{paths: []string{"a", "b", "c"}},
			wantOut: filepath.Join("/", "a", "b", "c"),
		},
		{
			name:    "sanitize dotdots",
			args:    args{paths: []string{"a/..", "b", "c"}},
			wantOut: filepath.Join("/", "b", "c"),
		},
		{
			name:    "leading dotdot",
			args:    args{paths: []string{"../a", "b", "c"}},
			wantOut: filepath.Join("/", "a", "b", "c"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotOut := securejoin(tt.args.paths...); gotOut != tt.wantOut {
				t.Errorf("securejoin() = %v, want %v", gotOut, tt.wantOut)
			}
		})
	}
}

func TestImageData_Store(t *testing.T) {
	tests := []struct {
		name    string
		image   *ImageData
		options *StorageOptions
		wantErr bool
	}{
		{
			name: "StoreImages false, should skip extraction",
			image: &ImageData{
				Reference:  "dummy.io/test/image:latest",
				Registry:   "dummy.io",
				Repository: "test/image",
				Tag:        "latest",
			},
			options: &StorageOptions{
				CachePath:     t.TempDir(),
				OutputPath:    t.TempDir(),
				StoreImages:   false,
				FilterSmall:   0,
				StoreTarballs: false,
			},
			wantErr: false,
		},
		{
			name: "StoreImages true, FilterSmall true, expect skip on big layers",
			image: &ImageData{
				Reference:  "dummy.io/test/image:latest",
				Registry:   "dummy.io",
				Repository: "test/image",
				Tag:        "latest",
				Manifest: `{
					"layers": [
						{"digest": "sha256:abc123", "size": 999999, "mediaType": "application/vnd.docker.image.rootfs.diff.tar.gzip"}
					]
				}`,
			},
			options: &StorageOptions{
				CachePath:     t.TempDir(),
				OutputPath:    t.TempDir(),
				StoreImages:   true,
				FilterSmall:   1,
				StoreTarballs: false,
			},
			wantErr: false,
		},
		{
			name: "StoreImages true, FilterSmall false, but empty Manifest",
			image: &ImageData{
				Reference:  "dummy.io/test/image:latest",
				Registry:   "dummy.io",
				Repository: "test/image",
				Tag:        "latest",
				Manifest:   `{}`,
			},
			options: &StorageOptions{
				CachePath:     t.TempDir(),
				OutputPath:    t.TempDir(),
				StoreImages:   true,
				FilterSmall:   0,
				StoreTarballs: true,
			},
			wantErr: false,
		},
		{
			name: "invalid manifest returns error",
			image: &ImageData{
				Reference:  "dummy.io/test/image:latest",
				Registry:   "dummy.io",
				Repository: "test/image",
				Tag:        "latest",
				Manifest:   "{notjson}",
			},
			options: &StorageOptions{
				CachePath:   t.TempDir(),
				OutputPath:  t.TempDir(),
				StoreImages: true,
			},
			wantErr: true,
		},
		{
			name: "skip when digest file exists",
			image: &ImageData{
				Reference:  "dummy.io/test/image:latest",
				Registry:   "dummy.io",
				Repository: "test/image",
				Tag:        "latest",
				Digest:     "sha256:deadbeef",
				Manifest:   `{}`,
			},
			options: func() *StorageOptions {
				out := &StorageOptions{
					CachePath:   t.TempDir(),
					OutputPath:  t.TempDir(),
					StoreImages: true,
				}
				os.MkdirAll(filepath.Join(out.OutputPath, "results"), 0755)
				os.WriteFile(filepath.Join(out.OutputPath, "results", "sha256_deadbeef"), []byte("hist"), 0644)
				return out
			}(),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.image.Store(tt.options)
			if (err != nil) != tt.wantErr {
				t.Errorf("ImageData.Store() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEnumImage(t *testing.T) {
	type args struct {
		reg     string
		repo    string
		tag     string
		options []crane.Option
	}
	tests := []struct {
		name string
		args args
	}{
		{name: "basic"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, repo, tag, cleanup := setupTestRegistry(t)
			defer cleanup()
			ch := EnumImage(host, repo, tag, crane.Insecure)
			img := <-ch
			if img.Error != nil {
				t.Fatalf("EnumImage error: %v", img.Error)
			}
			wantRef := fmt.Sprintf("%s/%s:%s", host, repo, tag)
			if img.Reference != wantRef {
				t.Errorf("got ref %s want %s", img.Reference, wantRef)
			}
		})
	}
}

func TestEnumRepository(t *testing.T) {
	type args struct {
		reg     string
		repo    string
		tags    []string
		options []crane.Option
	}
	tests := []struct {
		name string
		args args
	}{
		{name: "basic"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, repo, tag, cleanup := setupTestRegistry(t)
			defer cleanup()
			ch := EnumRepository(host, repo, []string{tag}, crane.Insecure)
			var count int
			for img := range ch {
				if img.Error != nil {
					t.Fatalf("EnumRepository error: %v", img.Error)
				}
				count++
			}
			if count != 1 {
				t.Errorf("expected 1 image, got %d", count)
			}
		})
	}
}

func TestEnumRegistry(t *testing.T) {
	type args struct {
		reg     string
		repos   []string
		tags    []string
		options []crane.Option
	}
	tests := []struct {
		name string
		args args
	}{
		{name: "basic"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, repo, tag, cleanup := setupTestRegistry(t)
			defer cleanup()
			ch := EnumRegistry(host, []string{repo}, []string{tag}, crane.Insecure)
			var count int
			for img := range ch {
				if img.Error != nil {
					t.Fatalf("EnumRegistry error: %v", img.Error)
				}
				count++
			}
			if count != 1 {
				t.Errorf("expected 1 image, got %d", count)
			}
		})
	}
}

func Test_bruteForceTags(t *testing.T) {
	type args struct {
		reg              string
		bruteForceConfig []byte
		options          []crane.Option
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{name: "empty result"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, _, _, cleanup := setupTestRegistry(t)
			defer cleanup()
			got := bruteForceTags(host, nil, crane.Insecure)
			if len(got) != 0 {
				t.Errorf("expected empty result, got %v", got)
			}
		})
	}
}

func TestEnumRegistries(t *testing.T) {
	type args struct {
		regs    []string
		repos   []string
		tags    []string
		options []crane.Option
	}
	tests := []struct {
		name string
		args args
	}{
		{name: "basic"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, repo, tag, cleanup := setupTestRegistry(t)
			defer cleanup()
			ch := EnumRegistries([]string{host}, []string{repo}, []string{tag}, crane.Insecure)
			var count int
			for img := range ch {
				if img.Error != nil {
					t.Fatalf("EnumRegistries error: %v", img.Error)
				}
				count++
			}
			if count != 1 {
				t.Errorf("expected 1 image, got %d", count)
			}
		})
	}
}

func TestCredentialSnippet(t *testing.T) {
	cfg := &authn.AuthConfig{Username: "user", Password: "secretpass"}
	got := CredentialSnippet(cfg)
	if !strings.Contains(got, "user user") || !strings.Contains(got, "pass") {
		t.Errorf("unexpected snippet: %s", got)
	}
	if !strings.Contains(got, "sec") {
		t.Errorf("credential snippet should contain password prefix, got: %s", got)
	}
}

func TestRunTruffleHog(t *testing.T) {
	type args struct {
		imageRef *ImageData
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{name: "success", wantErr: false},
		{name: "failure", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			exe := filepath.Join(dir, "trufflehog")
			var script string
			if tt.wantErr {
				script = "#!/bin/sh\nexit 1"
			} else {
				script = "#!/bin/sh\necho ok"
			}
			if err := os.WriteFile(exe, []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			oldPath := os.Getenv("PATH")
			os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
			defer os.Setenv("PATH", oldPath)
			img := &ImageData{Registry: "r", Repository: "repo", Tag: "tag"}
			err := RunTruffleHog(img)
			if (err != nil) != tt.wantErr {
				t.Errorf("RunTruffleHog() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
