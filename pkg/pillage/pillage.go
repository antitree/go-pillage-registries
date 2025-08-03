package pillage

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// ImageData represents an image enumerated from a registry or alternatively an error that occured while enumerating a registry.
type ImageData struct {
	Reference  string
	Registry   string
	Repository string
	Tag        string
	Manifest   string
	Config     string
	Error      error
	Image      v1.Image
}

// Manifest represents the image manifest layers metadata.
type Manifest struct {
	Layers []struct {
		Digest    string `json:"digest"`
		Size      int64  `json:"size"`
		MediaType string `json:"mediaType"`
	} `json:"layers"`
}

// StorageOptions configures caching, output paths, filtering, and storage behavior for image layers.
type StorageOptions struct {
	CachePath      string
	OutputPath     string
	StoreImages    bool
	CraneOptions   []crane.Option
	FilterSmall    int64
	StoreTarballs  bool
	WhiteOut       bool
	WhiteOutFilter []string
}

//go:embed default_config.json
var defaultConfigData []byte

// BruteForceConfig holds repository prefixes and names to probe when falling back to brute force enumeration.
type BruteForceConfig struct {
	Repos []string `json:"repos"`
	Names []string `json:"names"`
}

// FileVersion tracks the bytes of a file and the layer it was found in.
// FileVersion tracks the path to a temporary file containing the bytes of a
// file and the layer it was found in. Storing file contents on disk avoids
// keeping all file data in memory while processing large images.
type FileVersion struct {
	Layer    int
	Path     string
	TypeFlag byte
}

// MakeCraneOptions returns crane.Options for secure or insecure registry access
// and applies the provided authenticator or falls back to the default keychain.
func MakeCraneOptions(insecure bool, auth authn.Authenticator) (options []crane.Option) {
	if insecure {
		options = append(options, crane.Insecure)
	}
	if auth != nil {
		options = append(options, crane.WithAuth(auth))
	} else {
		// Fall back to the default keychain so any locally configured Docker
		// credentials are used automatically.
		options = append(options, crane.WithAuthFromKeychain(authn.DefaultKeychain))
	}
	return options
}

func securejoin(paths ...string) (out string) {
	for _, path := range paths {
		out = filepath.Join(out, filepath.Clean("/"+path))
	}
	return out
}

func shouldFilterWhiteout(name string, options *StorageOptions) bool {
	if len(options.WhiteOutFilter) == 0 {
		return false
	}
	lower := strings.ToLower(name)
	for _, pattern := range options.WhiteOutFilter {
		match, err := doublestar.PathMatch(strings.ToLower(pattern), lower)
		if err == nil && match {
			return true
		}
	}
	return false
}

// Store retrieves and processes the image layers according to StorageOptions.
// It handles caching, whiteout files, tarball storage, and error logging per layer.
func (image *ImageData) Store(options *StorageOptions) error {
	LogInfo("Pulling image layers for: %s", image.Reference)

	// Work on a copy of the options so concurrent calls do not race.
	opts := *options
	var cachePath string
	if opts.CachePath == "." {
		tmpDir, err := os.MkdirTemp("", "pilreg-tmp-")
		if err != nil {
			fmt.Println("Failed to create temp dir:", err)
			return err
		}
		defer os.RemoveAll(tmpDir) // clean up
		cachePath = tmpDir
	} else {
		cachePath = opts.CachePath
	}

	imagePath := filepath.Join(cachePath, securejoin(image.Registry, image.Repository, image.Tag))
	if err := os.MkdirAll(imagePath, os.ModePerm); err != nil {
		LogInfo("Error making storage path %s: %v", imagePath, err)
		return err
	}

	// Temporary directory used to store file versions so that memory usage
	// does not grow with the size of the image.
	tempDir := filepath.Join(imagePath, "filecache")
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return fmt.Errorf("error creating temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	if image.Error == nil {
		if opts.WhiteOut || opts.StoreImages || opts.StoreTarballs {
			var parsed Manifest
			err := json.Unmarshal([]byte(image.Manifest), &parsed)
			if err != nil {
				LogInfo("Error parsing manifest JSON for filtering: %v", err)
				return err
			}

			var previousFiles = make(map[string][]FileVersion)
			var imgLayers []v1.Layer
			if image.Image != nil {
				imgLayers, err = image.Image.Layers()
				if err != nil {
					LogInfo("Failed to get layers: %v", err)
					return err
				}
			}

			for idx, layer := range parsed.Layers {
				layerDir := filepath.Join(imagePath, strings.ReplaceAll(layer.Digest, ":", "_"))

				err := os.MkdirAll(layerDir, 0755)
				if err != nil {
					LogInfo("Failed to create dir %s: %v", layerDir, err)
					continue
				}

				if image.Image != nil {
					err = EnumLayerFromLayer(image, layerDir, imgLayers[idx], idx+1, &opts, previousFiles, tempDir)
				} else {
					layerRef := fmt.Sprintf("%s@%s", image.Reference, layer.Digest)
					err = EnumLayer(image, layerDir, layerRef, idx+1, &opts, opts.CraneOptions, previousFiles, tempDir)
				}
				if err != nil {
					LogWarn("Failed processing layer %s: %v", layer.Digest, err)
					LogDebug("%s\n%s", image.Manifest, image.Config)
					continue
				}
			}
		}

	}

	if image.Error != nil {
		errorPath := path.Join(imagePath, "errors.log")
		err := os.WriteFile(errorPath, []byte(image.Error.Error()), os.ModePerm)
		if err != nil {
			return fmt.Errorf("error making error file %s: %v", errorPath, err)
		}
	}
	return image.Error
}

// EnumLayer pulls the specified layer reference and unpacks it into a temporary cache,
// tracking previous versions for whiteout processing and optionally storing tarballs.
func EnumLayer(image *ImageData, layerDir, layerRef string, layerNumber int, storageOptions *StorageOptions, craneOpts []crane.Option, previousFiles map[string][]FileVersion, tempDir string) error {
	crLayer, err := crane.PullLayer(layerRef, craneOpts...)
	if err != nil {
		return fmt.Errorf("pull failed for layer %s: %w", layerRef, err)
	}

	rc, err := crLayer.Compressed()
	if err != nil {
		return fmt.Errorf("failed to get compressed stream: %w", err)
	}
	defer rc.Close()

	var tarReader *tar.Reader

	if storageOptions.StoreTarballs {
		tarPath := filepath.Join(layerDir, "filesystem.tar")
		f, err := os.Create(tarPath)
		if err != nil {
			return fmt.Errorf("cannot create tarball: %w", err)
		}
		if _, err := io.Copy(f, rc); err != nil {
			f.Close()
			return fmt.Errorf("failed writing tarball: %w", err)
		}
		f.Close()

		tarFile, err := os.Open(tarPath)
		if err != nil {
			return fmt.Errorf("cannot reopen tarball: %w", err)
		}
		defer tarFile.Close()

		gzr, err := gzip.NewReader(tarFile)
		if err != nil {
			return fmt.Errorf("gzip decompress failed: %w", err)
		}
		tarReader = tar.NewReader(gzr)
	} else {
		// Save original to close it later
		originalRC := rc
		defer originalRC.Close()

		// Peek first few bytes to check for gzip magic header
		buf := make([]byte, 2)
		if _, err := io.ReadFull(rc, buf); err != nil {
			return fmt.Errorf("peek error: %w", err)
		}

		// Restore peeked bytes into stream
		rc = io.NopCloser(io.MultiReader(bytes.NewReader(buf), rc))

		// Check for gzip magic numbers
		isGzip := buf[0] == 0x1f && buf[1] == 0x8b
		if isGzip {
			gzr, err := gzip.NewReader(rc)
			if err != nil {
				return fmt.Errorf("gzip decompress failed: %w", err)
			}
			defer gzr.Close()
			tarReader = tar.NewReader(gzr)
		} else {
			tarReader = tar.NewReader(rc)
		}

	}

	// Determine where to write restored files
	var resultsDir string
	resultsDir = filepath.Join(storageOptions.OutputPath, "results", securejoin(image.Registry, image.Repository, image.Tag))
	// Prepare the output path, but delay creation until needed
	createdResultsDir := false

	// if err := os.MkdirAll(resultsDir, 0755); err != nil {
	// 	return fmt.Errorf("failed to create results dir: %w", err)
	// }

	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			LogInfo("Error reading tar entry: %v", err)
			break
		}

		base := filepath.Base(hdr.Name)

		if strings.HasPrefix(base, ".wh.") {
			LogDebug("Whiteout file detected: ", hdr.Name)
			deletedPath := filepath.Join(filepath.Dir(hdr.Name), strings.TrimPrefix(base, ".wh."))
			deletedPath = strings.TrimPrefix(deletedPath, string(filepath.Separator))

			// Helper to create the results directory once
			ensureResultsDir := func() bool {
				if createdResultsDir {
					return true
				}
				if err := os.MkdirAll(resultsDir, 0755); err != nil {
					LogInfo("Failed to create dir for results: %v", err)
					return false
				}
				createdResultsDir = true
				return true
			}

			restoreFile := func(name string, data []byte, typ byte) {
				if shouldFilterWhiteout(name, storageOptions) {
					LogDebug("Skipping filtered whiteout file: %s", name)
					return
				}
				if len(data) == 0 && typ == tar.TypeReg {
					LogDebug("Skipping empty file: %s", name)
					return
				}
				if !ensureResultsDir() {
					return
				}
				sanitizedName := strings.TrimPrefix(filepath.Clean("/"+name), "/")
				restorePath := filepath.Join(resultsDir, fmt.Sprintf("%s.%d", sanitizedName, layerNumber))
				if err := os.MkdirAll(filepath.Dir(restorePath), 0755); err != nil {
					LogInfo("Failed to create dir for %s: %v", restorePath, err)
					return
				}
				f, err := os.Create(restorePath)
				if err != nil {
					LogInfo("Failed to create restore file %s: %v", restorePath, err)
					return
				}
				if _, err := f.Write(data); err != nil {
					LogInfo("Error restoring file %s: %v", restorePath, err)
				}
				f.Close()
				LogInfo("Restored whiteout-deleted file to %s", restorePath)
			}

			// Restore a single file if present
			if versions, ok := previousFiles[deletedPath]; ok && len(versions) > 0 {
				data, err := os.ReadFile(versions[len(versions)-1].Path)
				if err != nil {
					LogInfo("Error reading cached file %s: %v", versions[len(versions)-1].Path, err)
				} else {
					restoreFile(deletedPath, data, versions[len(versions)-1].TypeFlag)
				}
			} else {
				LogDebug("No previous version found for deleted file %s", deletedPath)
			}

			// Restore any files contained in a deleted directory
			prefix := deletedPath + string(filepath.Separator)
			for name, versions := range previousFiles {
				if strings.HasPrefix(name, prefix) && len(versions) > 0 {
					data, err := os.ReadFile(versions[len(versions)-1].Path)
					if err != nil {
						LogInfo("Error reading cached file %s: %v", versions[len(versions)-1].Path, err)
						continue
					}
					restoreFile(name, data, versions[len(versions)-1].TypeFlag)
				} else {
					LogDebug("No previous version found for deleted directory file %s", name)
				}
			}

			//} else if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeDir {
		} else {
			tempFile, err := os.CreateTemp(tempDir, "file-")
			if err != nil {
				LogInfo("Error creating temp file for %s: %v", hdr.Name, err)
				io.Copy(io.Discard, tarReader)
				continue
			}
			if _, err := io.Copy(tempFile, tarReader); err == nil {
				tempFile.Close()
				name := strings.TrimPrefix(hdr.Name, string(filepath.Separator))
				previousFiles[name] = append(previousFiles[name], FileVersion{Layer: layerNumber, Path: tempFile.Name(), TypeFlag: hdr.Typeflag})
			} else {
				tempFile.Close()
				os.Remove(tempFile.Name())
				LogInfo("Error reading file %s from tar: %v", hdr.Name, err)
				continue
			}
		}
	}

	return nil
}

// EnumLayerFromLayer processes an already fetched layer object similarly to EnumLayer,
// extracting files and tracking whiteout operations.
func EnumLayerFromLayer(image *ImageData, layerDir string, layer v1.Layer, layerNumber int, storageOptions *StorageOptions, previousFiles map[string][]FileVersion, tempDir string) error {
	rc, err := layer.Compressed()
	if err != nil {
		return fmt.Errorf("failed to get compressed stream: %w", err)
	}
	defer rc.Close()

	var tarReader *tar.Reader

	if storageOptions.StoreTarballs {
		tarPath := filepath.Join(layerDir, "filesystem.tar")
		f, err := os.Create(tarPath)
		if err != nil {
			return fmt.Errorf("cannot create tarball: %w", err)
		}
		if _, err := io.Copy(f, rc); err != nil {
			f.Close()
			return fmt.Errorf("failed writing tarball: %w", err)
		}
		f.Close()

		tarFile, err := os.Open(tarPath)
		if err != nil {
			return fmt.Errorf("cannot reopen tarball: %w", err)
		}
		defer tarFile.Close()

		gzr, err := gzip.NewReader(tarFile)
		if err != nil {
			return fmt.Errorf("gzip decompress failed: %w", err)
		}
		tarReader = tar.NewReader(gzr)
	} else {
		originalRC := rc
		defer originalRC.Close()

		buf := make([]byte, 2)
		if _, err := io.ReadFull(rc, buf); err != nil {
			return fmt.Errorf("peek error: %w", err)
		}

		rc = io.NopCloser(io.MultiReader(bytes.NewReader(buf), rc))

		isGzip := buf[0] == 0x1f && buf[1] == 0x8b
		if isGzip {
			gzr, err := gzip.NewReader(rc)
			if err != nil {
				return fmt.Errorf("gzip decompress failed: %w", err)
			}
			defer gzr.Close()
			tarReader = tar.NewReader(gzr)
		} else {
			tarReader = tar.NewReader(rc)
		}

	}

	var resultsDir string
	resultsDir = filepath.Join(storageOptions.OutputPath, "results", securejoin(image.Registry, image.Repository, image.Tag))
	createdResultsDir := false

	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			LogInfo("Error reading tar entry: %v", err)
			break
		}

		base := filepath.Base(hdr.Name)

		if strings.HasPrefix(base, ".wh.") {
			LogDebug("Whiteout file detected: ", hdr.Name)
			deletedPath := filepath.Join(filepath.Dir(hdr.Name), strings.TrimPrefix(base, ".wh."))
			deletedPath = strings.TrimPrefix(deletedPath, string(filepath.Separator))

			ensureResultsDir := func() bool {
				if createdResultsDir {
					return true
				}
				if err := os.MkdirAll(resultsDir, 0755); err != nil {
					LogInfo("Failed to create dir for results: %v", err)
					return false
				}
				createdResultsDir = true
				return true
			}

			restoreFile := func(name string, data []byte, typ byte) {
				if shouldFilterWhiteout(name, storageOptions) {
					LogDebug("Skipping filtered whiteout file: %s", name)
					return
				}
				if len(data) == 0 && typ == tar.TypeReg {
					LogDebug("Skipping empty file: %s", name)
					return
				}
				if !ensureResultsDir() {
					return
				}
				sanitizedName := strings.TrimPrefix(filepath.Clean("/"+name), "/")
				restorePath := filepath.Join(resultsDir, fmt.Sprintf("%s.%d", sanitizedName, layerNumber))
				if err := os.MkdirAll(filepath.Dir(restorePath), 0755); err != nil {
					LogInfo("Failed to create dir for %s: %v", restorePath, err)
					return
				}
				f, err := os.Create(restorePath)
				if err != nil {
					LogInfo("Failed to create restore file %s: %v", restorePath, err)
					return
				}
				if _, err := f.Write(data); err != nil {
					LogInfo("Error restoring file %s: %v", restorePath, err)
				}
				f.Close()
				LogInfo("Restored whiteout-deleted file to %s", restorePath)
			}

			if versions, ok := previousFiles[deletedPath]; ok && len(versions) > 0 {
				data, err := os.ReadFile(versions[len(versions)-1].Path)
				if err != nil {
					LogInfo("Error reading cached file %s: %v", versions[len(versions)-1].Path, err)
				} else {
					restoreFile(deletedPath, data, versions[len(versions)-1].TypeFlag)
				}
			} else {
				LogDebug("No previous version found for deleted file %s", deletedPath)
			}

			prefix := deletedPath + string(filepath.Separator)
			for name, versions := range previousFiles {
				if strings.HasPrefix(name, prefix) && len(versions) > 0 {
					data, err := os.ReadFile(versions[len(versions)-1].Path)
					if err != nil {
						LogInfo("Error reading cached file %s: %v", versions[len(versions)-1].Path, err)
						continue
					}
					restoreFile(name, data, versions[len(versions)-1].TypeFlag)
				} else {
					LogDebug("No previous version found for deleted directory file %s", name)
				}
			}

		} else {
			tempFile, err := os.CreateTemp(tempDir, "file-")
			if err != nil {
				LogInfo("Error creating temp file for %s: %v", hdr.Name, err)
				io.Copy(io.Discard, tarReader)
				continue
			}
			if _, err := io.Copy(tempFile, tarReader); err == nil {
				tempFile.Close()
				name := strings.TrimPrefix(hdr.Name, string(filepath.Separator))
				previousFiles[name] = append(previousFiles[name], FileVersion{Layer: layerNumber, Path: tempFile.Name(), TypeFlag: hdr.Typeflag})
			} else {
				tempFile.Close()
				os.Remove(tempFile.Name())
				LogInfo("Error reading file %s from tar: %v", hdr.Name, err)
				continue
			}
		}
	}

	return nil
}

// EnumImage will read a specific image from a remote registry and returns the result asynchronously.
func EnumImage(reg string, repo string, tag string, options ...crane.Option) <-chan *ImageData {
	out := make(chan *ImageData)

	ref := fmt.Sprintf("%s/%s:%s", reg, repo, tag)

	go func(ref string) {
		defer close(out)

		result := &ImageData{
			Reference:  ref,
			Registry:   reg,
			Repository: repo,
			Tag:        tag,
		}

		var manifest Manifest

		var unparsedmanifest []byte
		err := retryWithBackoff(5, 60*time.Second, func() error {
			m, err := crane.Manifest(ref, options...)
			if err == nil {
				unparsedmanifest = m
			}
			return err
		})
		if err != nil {
			LogError("Error fetching manifest for image %s: %s", ref, err)
			result.Error = err
		}

		err = json.Unmarshal([]byte(unparsedmanifest), &manifest)
		if err != nil {
			LogInfo("Error parsing manifest for image %s: %s", ref, err)
			result.Error = err
		}

		strManifest, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			LogInfo("Error fetching parsing Manifest for image %s: %s", ref, err)
		}
		result.Manifest = string(strManifest)

		var config []byte
		err = retryWithBackoff(5, 60*time.Second, func() error {
			m, err := crane.Config(ref, options...)
			if err == nil {
				config = m
			}
			return err
		})

		if err != nil {
			LogInfo("Error fetching config for image %s: %s (the config may be in the manifest itself)", ref, err)

			errStr := err.Error()
			if strings.Contains(errStr, "TOOMANYREQUESTS") ||
				strings.Contains(errStr, "Rate exceeded") ||
				strings.Contains(errStr, "429") {
				log.Fatalf("Fatal: rate limited on %s: %v", ref, err)
			}
		}
		result.Config = string(config)

		out <- result
	}(ref)

	return out
}

// EnumRepository will read all images tagged in a specific repository on a remote registry and returns the results asynchronously.
// If a list of tags is not supplied, a list will be enumerated from the registry's API.
func EnumRepository(reg string, repo string, tags []string, options ...crane.Option) <-chan *ImageData {
	out := make(chan *ImageData)
	ref := fmt.Sprintf("%s/%s", reg, repo)
	LogInfo("Repo: %s", ref)

	go func(ref string) {
		defer close(out)

		if len(tags) == 0 {
			var err error
			tags, err = crane.ListTags(ref, options...)

			if err != nil {
				// Classify connection error
				errStr := err.Error()
				if strings.Contains(errStr, "connection refused") ||
					strings.Contains(errStr, "no such host") ||
					strings.Contains(errStr, "dial tcp") {
					log.Fatalf("Fatal: cannot reach registry for %s: %v", ref, err)
				}

				LogError("Error listing tags for %s: %s", ref, err)
				out <- &ImageData{
					Reference:  ref,
					Registry:   reg,
					Repository: repo,
					Error:      err,
				}
			}
		}

		var wg sync.WaitGroup

		for _, tag := range tags {
			wg.Add(1)
			go func(tag string) {
				defer wg.Done()
				images := EnumImage(reg, repo, tag, options...)
				for image := range images {
					out <- image
				}
			}(tag)
		}

		wg.Wait()
		return
	}(ref)
	return out
}

// EnumRegistry will read all images cataloged on a remote registry and returns the results asynchronously.
// If lists of repositories and tags are not supplied, lists will be enumerated from the registry's API.
func EnumRegistry(reg string, repos []string, tags []string, options ...crane.Option) <-chan *ImageData {
	out := make(chan *ImageData)
	LogInfo("Registry: %s\n", reg)

	bruteForceFile := defaultConfigData

	go func() {
		defer close(out)
		var err error

		if len(repos) == 0 {
			repos, err = crane.Catalog(reg, options...)
			// log.Print(repos)
			if err != nil {
				LogError("Error listing repos for %s: (%T) %s", reg, err, err)
				LogWarn("Catalog API not available. Falling back to brute force enumeration.")
				repos = bruteForceTags(reg, bruteForceFile, options...)
			}
		}

		var wg sync.WaitGroup

		for _, repo := range repos {
			wg.Add(1)
			go func(repo string) {
				defer wg.Done()
				images := EnumRepository(reg, repo, tags, options...)
				for image := range images {
					out <- image
				}
			}(repo)
		}

		wg.Wait()
	}()
	return out
}

// Brute forces common repo names to see if they exist in the registry. This includes pass-through
// configured names. Modify the default_config.json for specific combinations
func bruteForceTags(reg string, bruteForceConfig []byte, options ...crane.Option) []string {
	var tags []string

	var config BruteForceConfig
	if err := json.NewDecoder(bytes.NewReader(defaultConfigData)).Decode(&config); err != nil {
		LogInfo("Error decoding embedded config: %s", err)
		return tags
	}

	for _, repoPrefix := range config.Repos {
		LogInfo("Bruteforcing %s repos", repoPrefix)
		for _, name := range config.Names {

			ref := fmt.Sprintf("%s/%s", reg, path.Join(repoPrefix, name))

			_, err := crane.Manifest(ref, options...) // what does this do??
			if err == nil {
				tags = append(tags, path.Join(repoPrefix, name))
			}
		}
	}

	return tags
}

// EnumRegistries will read all images cataloged by a set of remote registries and returns the results asynchronously.
// If lists of repositories and tags are not supplied, lists will be enumerated from the registry's API.
func EnumRegistries(regs []string, repos []string, tags []string, options ...crane.Option) <-chan *ImageData {
	out := make(chan *ImageData)
	go func() {
		defer close(out)

		if len(regs) == 0 {
			err := errors.New("No Registries supplied")
			log.Println(err)
			out <- &ImageData{
				Reference: "",
				Error:     err,
			}
			return
		}

		var wg sync.WaitGroup

		for _, reg := range regs {
			wg.Add(1)
			go func(reg string) {
				defer wg.Done()
				images := EnumRegistry(reg, repos, tags, options...)
				for image := range images {
					out <- image
				}
			}(reg)

		}
		wg.Wait()
	}()
	return out
}

// EnumTarball reads a docker image tarball saved with 'docker save' and returns images found within.
func EnumTarball(tarPath string) <-chan *ImageData {
	out := make(chan *ImageData)
	go func() {
		defer close(out)

		opener := func() (io.ReadCloser, error) { return os.Open(tarPath) }

		manifest, err := tarball.LoadManifest(opener)
		if err != nil {
			out <- &ImageData{Reference: tarPath, Error: err}
			return
		}

		for _, desc := range manifest {
			for _, tagStr := range desc.RepoTags {
				tag, err := name.NewTag(tagStr, name.WeakValidation)
				if err != nil {
					out <- &ImageData{Reference: tagStr, Error: err}
					continue
				}

				img, err := tarball.Image(opener, &tag)
				if err != nil {
					out <- &ImageData{Reference: tagStr, Error: err}
					continue
				}

				man, err := img.RawManifest()
				if err != nil {
					out <- &ImageData{Reference: tagStr, Error: err}
					continue
				}
				cfg, err := img.RawConfigFile()
				if err != nil {
					out <- &ImageData{Reference: tagStr, Error: err}
					continue
				}

				sanitizedRef := fmt.Sprintf("%s:%s", tag.Repository.RepositoryStr(), tag.TagStr())

				out <- &ImageData{
					Reference:  sanitizedRef,
					Registry:   tag.RegistryStr(),
					Repository: tag.RepositoryStr(),
					Tag:        tag.TagStr(),
					Manifest:   string(man),
					Config:     string(cfg),
					Image:      img,
				}
			}
		}
	}()

	return out
}

// ValidateTarball checks that the provided path points to a valid tar archive.
func ValidateTarball(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, 2)
	if _, err := io.ReadFull(f, buf); err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}

	var r io.Reader = f
	if buf[0] == 0x1f && buf[1] == 0x8b {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gz.Close()
		r = gz
	}

	tr := tar.NewReader(r)
	if _, err := tr.Next(); err != nil {
		return fmt.Errorf("invalid tarball: %w", err)
	}
	return nil
}

// RunTruffleHog invokes the trufflehog binary against a Docker image reference,
// logging the output and returning an error on non-zero exit.
func RunTruffleHog(imageRef *ImageData) error {
	image := securejoin(imageRef.Registry, imageRef.Repository, imageRef.Tag)
	cmd := exec.Command("trufflehog", "docker", fmt.Sprintf("--image=%s", image))
	output, err := cmd.CombinedOutput()
	if err != nil {
		LogInfo("trufflehog failed for %s: %v\nOutput:\n%s", image, err, string(output))
		return err
	}
	LogInfo("trufflehog completed for %s:\n%s", image, string(output))
	return nil
}

// retryWithBackoff executes op up to attempts times with exponential backoff and jitter.
// It aborts immediately on authentication errors and returns the last encountered error.
func retryWithBackoff(attempts int, baseDelay time.Duration, op func() error) error {
	delay := baseDelay

	for i := 0; i < attempts; i++ {
		err := op()
		if err == nil {
			return nil
		}
		// If authentication error, do not retry; skip permanently
		errStr := err.Error()
		if strings.Contains(errStr, "UNAUTHORIZED") || strings.Contains(errStr, "authentication required") {
			LogWarn("Skipping retry due to authentication error: %v", err)
			return err
		}

		if i < attempts-1 {
			// Add jitter (up to 50% of delay)
			jitter := time.Duration(rand.Int63n(int64(delay) / 2))
			sleep := delay + jitter

			LogInfo("Retrying after %v due to error: %v", sleep, err)
			time.Sleep(sleep)
			delay *= 2
		} else {
			return err
		}
	}

	return errors.New("max retries exceeded")
}
