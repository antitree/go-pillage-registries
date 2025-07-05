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

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
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
}

type Manifest struct {
	Layers []struct {
		Digest    string `json:"digest"`
		Size      int64  `json:"size"`
		MediaType string `json:"mediaType"`
	} `json:"layers"`
}

type StorageOptions struct {
	CachePath     string
	OutputPath    string
	StoreImages   bool
	CraneOptions  []crane.Option
	FilterSmall   int64
	StoreTarballs bool
	WhiteOut      bool
}

//go:embed default_config.json
var defaultConfigData []byte

type BruteForceConfig struct {
	Repos []string `json:"repos"`
	Names []string `json:"names"`
}

// FileVersion tracks the bytes of a file and the layer it was found in.
type FileVersion struct {
	Layer int
	Data  []byte
}

func MakeCraneOptions(insecure bool) (options []crane.Option) {
	if insecure {
		options = append(options, crane.Insecure)
	}
	return options
}

func securejoin(paths ...string) (out string) {
	for _, path := range paths {
		out = filepath.Join(out, filepath.Clean("/"+path))
	}
	return out
}

func (image *ImageData) Store(options *StorageOptions) error {
	log.Printf("Pulling image layers for: %s", image.Reference)
	var imagePath string

	if options.CachePath == "." {
		tmpDir, err := os.MkdirTemp("", "pilreg-tmp-")
		if err != nil {
			fmt.Println("Failed to create temp dir:", err)
			return err
		}

		defer os.RemoveAll(tmpDir) // clean up

		options.CachePath = tmpDir
	}

	imagePath = filepath.Join(options.CachePath, securejoin(image.Registry, image.Repository, image.Tag))
	if err := os.MkdirAll(imagePath, os.ModePerm); err != nil {
		log.Printf("Error making storage path %s: %v", imagePath, err)
		return err
	}

	if image.Error == nil {
		if options.WhiteOut || options.StoreImages || options.StoreTarballs {
			var parsed Manifest
			err := json.Unmarshal([]byte(image.Manifest), &parsed)
			if err != nil {
				log.Printf("Error parsing manifest JSON for filtering: %v", err)
				return err
			}

			var previousFiles = make(map[string][]FileVersion)
			for idx, layer := range parsed.Layers {
				// whiteout files are small
				// if layer.Size > options.FilterSmall {
				// 	continue
				// }

				layerDir := filepath.Join(imagePath, strings.ReplaceAll(layer.Digest, ":", "_"))

				err := os.MkdirAll(layerDir, 0755)
				if err != nil {
					log.Printf("Failed to create dir %s: %v", layerDir, err)
					continue
				}

				layerRef := fmt.Sprintf("%s@%s", image.Reference, layer.Digest)

				err = EnumLayer(image, layerDir, layerRef, idx+1, options, options.CraneOptions, previousFiles)
				if err != nil {
					LogWarn("Failed processing layer %s: %v", layer.Digest, err)
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

func EnumLayer(image *ImageData, layerDir, layerRef string, layerNumber int, storageOptions *StorageOptions, craneOpts []crane.Option, previousFiles map[string][]FileVersion) error {
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
			log.Printf("Error reading tar entry: %v", err)
			break
		}

		base := filepath.Base(hdr.Name)

		if strings.HasPrefix(base, ".wh.") {
			log.Print("Fucking whiteout file detected: ", hdr.Name)
			deletedPath := filepath.Join(filepath.Dir(hdr.Name), strings.TrimPrefix(base, ".wh."))
			deletedPath = strings.TrimPrefix(deletedPath, string(filepath.Separator))

			// Helper to create the results directory once
			ensureResultsDir := func() bool {
				if createdResultsDir {
					return true
				}
				if err := os.MkdirAll(resultsDir, 0755); err != nil {
					log.Printf("Failed to create dir for results: %v", err)
					return false
				}
				createdResultsDir = true
				return true
			}

			restoreFile := func(name string, data []byte) {
				if !ensureResultsDir() {
					return
				}
				restorePath := filepath.Join(resultsDir, fmt.Sprintf("%s.%d", name, layerNumber))
				if err := os.MkdirAll(filepath.Dir(restorePath), 0755); err != nil {
					log.Printf("Failed to create dir for %s: %v", restorePath, err)
					return
				}
				f, err := os.Create(restorePath)
				if err != nil {
					log.Printf("Failed to create restore file %s: %v", restorePath, err)
					return
				}
				if _, err := f.Write(data); err != nil {
					log.Printf("Error restoring file %s: %v", restorePath, err)
				}
				f.Close()
				log.Printf("Restored whiteout-deleted file to %s", restorePath)
			}

			// Restore a single file if present
			if versions, ok := previousFiles[deletedPath]; ok && len(versions) > 0 {
				data := versions[len(versions)-1].Data
				restoreFile(deletedPath, data)
			} else {
				log.Printf("No previous version found for deleted file %s", deletedPath)
			}

			// Restore any files contained in a deleted directory
			prefix := deletedPath + string(filepath.Separator)
			for name, versions := range previousFiles {
				if strings.HasPrefix(name, prefix) && len(versions) > 0 {
					data := versions[len(versions)-1].Data
					restoreFile(name, data)
				} else {
					log.Printf("No previous version found for deleted directory file %s", name)
				}
			}

			//} else if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeDir {
		} else {
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, tarReader); err == nil {
				name := strings.TrimPrefix(hdr.Name, string(filepath.Separator))
				previousFiles[name] = append(previousFiles[name], FileVersion{Layer: layerNumber, Data: buf.Bytes()})
			} else {
				log.Printf("Error reading file %s from tar: %v", hdr.Name, err)
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
			log.Printf("Error parsing manifest for image %s: %s", ref, err)
			result.Error = err
		}

		// HACK this was a test for whiteout detection. This shinks the layers but it's arbitrary
		// TODO refactor
		// for i := 0; i < len(manifest.Layers); {
		// 	if manifest.Layers[i].Size > 40000 {
		// 		manifest.Layers = append(manifest.Layers[:i], manifest.Layers[i+1:]...)
		// 	} else {
		// 		i++
		// 	}
		// }

		strManifest, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			log.Printf("Error fetching parsing Manifest for image %s: %s", ref, err)
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
		//config, err := crane.Config(ref, options...)
		if err != nil {
			log.Printf("Error fetching config for image %s: %s (the config may be in the manifest itself)", ref, err)

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
	log.Printf("Repo: %s", ref)

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
	log.Printf("Registry: %s\n", reg)

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
		log.Printf("Error decoding embedded config: %s", err)
		return tags
	}

	for _, repoPrefix := range config.Repos {
		log.Printf("Bruteforcing %s repos", repoPrefix)
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
					Registry:   "",
					Repository: tag.Repository.RepositoryStr(),
					Tag:        tag.TagStr(),
					Manifest:   string(man),
					Config:     string(cfg),
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

func RunTruffleHog(imageRef *ImageData) error {
	image := securejoin(imageRef.Registry, imageRef.Repository, imageRef.Tag)
	cmd := exec.Command("trufflehog", "docker", fmt.Sprintf("--image=%s", image))
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("trufflehog failed for %s: %v\nOutput:\n%s", image, err, string(output))
		return err
	}
	log.Printf("trufflehog completed for %s:\n%s", image, string(output))
	return nil
}

func retryWithBackoff(attempts int, baseDelay time.Duration, op func() error) error {
	delay := baseDelay

	for i := 0; i < attempts; i++ {
		err := op()
		if err == nil {
			return nil
		}

		if i < attempts-1 {
			// Add jitter (up to 50% of delay)
			jitter := time.Duration(rand.Int63n(int64(delay) / 2))
			sleep := delay + jitter

			log.Printf("Retrying after %v due to error: %v", sleep, err)
			time.Sleep(sleep)
			delay *= 2
			// if delay > 30*time.Second {
			// 	delay = 30 * time.Second // Cap max delay
			// }
		} else {
			return err
		}
	}

	return errors.New("max retries exceeded")
}
