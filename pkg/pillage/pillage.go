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
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/go-containerregistry/pkg/crane"
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
	FilterSmall   bool
	StoreTarballs bool
}

//go:embed default_config.json
var defaultConfigData []byte

type BruteForceConfig struct {
	Repos []string `json:"repos"`
	Names []string `json:"names"`
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
	log.Printf("Storing results for image: %s", image.Reference)
	var imagePath string

	if options.CachePath == "" {
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
		if options.FilterSmall {
			var parsed Manifest
			err := json.Unmarshal([]byte(image.Manifest), &parsed)
			if err != nil {
				log.Printf("Error parsing manifest JSON for filtering: %v", err)
				return err
			}

			var previousFiles = make(map[string][]byte)
			for _, layer := range parsed.Layers {
				// whiteout files are small
				if layer.Size > 40000 && options.FilterSmall {
					continue
				}

				layerDir := filepath.Join(imagePath, strings.ReplaceAll(layer.Digest, ":", "_"))

				err := os.MkdirAll(layerDir, 0755)
				if err != nil {
					log.Printf("Failed to create dir %s: %v", layerDir, err)
					continue
				}

				// TODO THERE'S SOME KIND OF LOGIC HERE YOU NEED TO FIX
				// IT'S CREATING A FILESYSTEM.TAR FILE SO THAT YOU CAN
				// DECOMPRESS IT BUT YOU DON'T NEED THIS USUALLY. MAYBER EVER
				// COME UP WITH AN OPTIONAL STORE OF THE FILESYSTEM.TAR OR
				// DELETE IT AFTER THE DECOMPRESS HAPPENS.
				//filePath := filepath.Join(layerDir, "filesystem.tar")
				layerRef := fmt.Sprintf("%s@%s", image.Reference, layer.Digest)

				err = EnumLayer(image, layerDir, layerRef, options, options.CraneOptions, previousFiles)
				if err != nil {
					LogWarn("Failed processing layer %s: %v", layer.Digest, err)
					continue
				}
				// 	crLayer, err := crane.PullLayer(layerRef, options.CraneOptions...)
				// 	if err != nil {
				// 		log.Printf("Failed to pull layer %s: %v", layer.Digest, err)
				// 		continue
				// 	}

				// 	rc, err := crLayer.Compressed()
				// 	if err != nil {
				// 		log.Printf("Failed to get compressed stream for %s: %v", layer.Digest, err)
				// 		continue
				// 	}
				// 	f, err := os.Create(filePath)
				// 	if err != nil {
				// 		rc.Close()
				// 		log.Printf("Failed to create layer file %s: %v", filePath, err)
				// 		continue
				// 	}
				// 	_, err = io.Copy(f, rc)
				// 	rc.Close()
				// 	f.Close()
				// 	if err != nil {
				// 		log.Printf("Error saving layer file: %v", err)
				// 		continue
				// 	}

				// 	tarF, err := os.Open(filePath)
				// 	if err != nil {
				// 		log.Printf("Failed to open tar file %s: %v", filePath, err)
				// 		continue
				// 	}
				// 	gzr, err := gzip.NewReader(tarF)
				// 	if err != nil {
				// 		tarF.Close()
				// 		log.Printf("Failed to create gzip reader for %s: %v", filePath, err)
				// 		continue
				// 	}
				// 	tarReader := tar.NewReader(gzr)

				// 	for {
				// 		hdr, err := tarReader.Next()
				// 		if err == io.EOF {
				// 			break
				// 		}
				// 		if err != nil {
				// 			log.Printf("Error reading tar entry: %v", err)
				// 			break
				// 		}

				// 		base := filepath.Base(hdr.Name)
				// 		if strings.HasPrefix(base, ".wh.") {
				// 			deletedFile := strings.TrimPrefix(base, ".wh.")
				// 			if data, ok := previousFiles[deletedFile]; ok {
				// 				restorePath := filepath.Join(layerDir, deletedFile)
				// 				os.MkdirAll(filepath.Dir(restorePath), 0755)
				// 				restoreFile, err := os.Create(restorePath)
				// 				if err != nil {
				// 					log.Printf("Failed to create restore file %s: %v", restorePath, err)
				// 					continue
				// 				}
				// 				_, err = restoreFile.Write(data)
				// 				restoreFile.Close()
				// 				if err != nil {
				// 					log.Printf("Error writing restored file %s: %v", restorePath, err)
				// 				} else {
				// 					log.Printf("Whiteout file found %s from %s", deletedFile, hdr.Name)
				// 				}
				// 			}
				// 		} else if hdr.Typeflag == tar.TypeReg {
				// 			var buf bytes.Buffer
				// 			_, err := io.Copy(&buf, tarReader)
				// 			if err == nil {
				// 				previousFiles[filepath.Base(hdr.Name)] = buf.Bytes()
				// 			}
				// 		}
				// 	}
				// 	tarF.Close()
			}
		}

	}

	if image.Error != nil {
		errorPath := path.Join(imagePath, "errors.log")
		err := ioutil.WriteFile(errorPath, []byte(image.Error.Error()), os.ModePerm)
		if err != nil {
			return fmt.Errorf("error making error file %s: %v", errorPath, err)
		}
	}
	return image.Error
}

func EnumLayer(image *ImageData, layerDir, layerRef string, storageOptions *StorageOptions, craneOpts []crane.Option, previousFiles map[string][]byte) error {
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
		gzr, err := gzip.NewReader(rc)
		if err != nil {
			return fmt.Errorf("gzip decompress failed: %w", err)
		}
		tarReader = tar.NewReader(gzr)
	}

	// Determine where to write restored files
	var resultsDir string
	resultsDir = filepath.Join(storageOptions.OutputPath, "results", securejoin(image.Registry, image.Repository, image.Tag))

	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		return fmt.Errorf("failed to create results dir: %w", err)
	}

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
			deletedFile := strings.TrimPrefix(base, ".wh.")
			if data, ok := previousFiles[deletedFile]; ok {
				restorePath := filepath.Join(resultsDir, deletedFile)
				if err := os.MkdirAll(filepath.Dir(restorePath), 0755); err != nil {
					log.Printf("Failed to create dir for %s: %v", restorePath, err)
					continue
				}
				restoreFile, err := os.Create(restorePath)
				if err != nil {
					log.Printf("Failed to create restore file %s: %v", restorePath, err)
					continue
				}
				if _, err := restoreFile.Write(data); err != nil {
					log.Printf("Error restoring file %s: %v", restorePath, err)
				}
				restoreFile.Close()
				log.Printf("Restored whiteout-deleted file to %s", restorePath)
			}
		} else if hdr.Typeflag == tar.TypeReg {
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, tarReader); err == nil {
				previousFiles[filepath.Base(hdr.Name)] = buf.Bytes()
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

		unparsedmanifest, err := crane.Manifest(ref, options...)
		if err != nil {
			LogError("Error fetching manifest for image %s: %s", ref, err)
			result.Error = err
		}

		err = json.Unmarshal([]byte(unparsedmanifest), &manifest)
		if err != nil {
			log.Printf("Error parsing manifest for image %s: %s", ref, err)
			result.Error = err
		}

		for i := 0; i < len(manifest.Layers); {
			if manifest.Layers[i].Size > 40000 {
				manifest.Layers = append(manifest.Layers[:i], manifest.Layers[i+1:]...)
			} else {
				i++
			}
		}

		strManifest, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			log.Printf("Error fetching parsing Manifest for image %s: %s", ref, err)
		}
		result.Manifest = string(strManifest)

		config, err := crane.Config(ref, options...)
		if err != nil {
			log.Printf("Error fetching config for image %s: %s (the config may be in the manifest itself)", ref, err)
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

			_, err := crane.Manifest(ref, options...)
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
