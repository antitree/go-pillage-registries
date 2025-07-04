package pillage

import (
	_ "embed"
	"reflect"
	"testing"

	"github.com/google/go-containerregistry/pkg/crane"
)

func TestMakeCraneOptions(t *testing.T) {
	type args struct {
		insecure bool
	}
	tests := []struct {
		name        string
		args        args
		wantOptions []crane.Option
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotOptions := MakeCraneOptions(tt.args.insecure); !reflect.DeepEqual(gotOptions, tt.wantOptions) {
				t.Errorf("MakeCraneOptions() = %v, want %v", gotOptions, tt.wantOptions)
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
		// TODO: Add test cases.
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
		want <-chan *ImageData
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EnumImage(tt.args.reg, tt.args.repo, tt.args.tag, tt.args.options...); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("EnumImage() = %v, want %v", got, tt.want)
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
		want <-chan *ImageData
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EnumRepository(tt.args.reg, tt.args.repo, tt.args.tags, tt.args.options...); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("EnumRepository() = %v, want %v", got, tt.want)
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
		want <-chan *ImageData
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EnumRegistry(tt.args.reg, tt.args.repos, tt.args.tags, tt.args.options...); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("EnumRegistry() = %v, want %v", got, tt.want)
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
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bruteForceTags(tt.args.reg, tt.args.bruteForceConfig, tt.args.options...); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("bruteForceTags() = %v, want %v", got, tt.want)
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
		want <-chan *ImageData
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EnumRegistries(tt.args.regs, tt.args.repos, tt.args.tags, tt.args.options...); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("EnumRegistries() = %v, want %v", got, tt.want)
			}
		})
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
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := RunTruffleHog(tt.args.imageRef); (err != nil) != tt.wantErr {
				t.Errorf("RunTruffleHog() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
