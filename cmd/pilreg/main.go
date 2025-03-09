package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"

	"github.com/remeh/sizedwaitgroup"

	"github.com/antitree/go-pillage-registries/pkg/pillage"
	"github.com/spf13/cobra"
)

var (
	repos       []string
	tags        []string
	skiptls     bool
	insecure    bool
	storeImages bool
	registry    string
	cachePath   string
	resultsPath string
	workerCount int
	truffleHog  bool
)

func init() {
	rootCmd.PersistentFlags().StringSliceVarP(&repos, "repos", "r", []string{}, "list of repositories to scan on the registry. If blank, pilreg will attempt to enumerate them using the catalog API")
	rootCmd.PersistentFlags().StringSliceVarP(&tags, "tags", "t", []string{}, "list of tags to scan on each repository. If blank, pilreg will attempt to enumerate them using the tags API")

	rootCmd.PersistentFlags().StringVarP(&resultsPath, "results", "o", "", "Path to directory for storing results. If blank, outputs configs and manifests as json object to Stdout.(must be used if 'store-images` is enabled)")
	rootCmd.PersistentFlags().BoolVarP(&skiptls, "skip-tls", "k", false, "Disables TLS certificate verification")
	rootCmd.PersistentFlags().BoolVarP(&insecure, "insecure", "i", false, "Fetch Data over plaintext")
	rootCmd.PersistentFlags().BoolVarP(&storeImages, "store-images", "s", false, "Downloads filesystem for discovered images and stores an archive in the output directory (Disabled by default, requires --results to be set)")
	rootCmd.PersistentFlags().StringVarP(&cachePath, "cache", "c", "", "Path to cache image layers (optional, only used if images are pulled)")
	rootCmd.PersistentFlags().IntVarP(&workerCount, "workers", "w", 8, "Number of workers when pulling images. If set too high, this may cause errors. (optional, only used if images are pulled)")
	rootCmd.PersistentFlags().BoolVarP(&truffleHog, "trufflehog", "x", false, "Integrate with Trufflehog to scan the images once they are found")
	// rootCmd.PersistentFlags().StringVar(&bruteForceConfigFile, "config", "", "Path to brute force config JSON file (optional)")
}

var rootCmd = &cobra.Command{
	Use:   "pilreg <registry>",
	Short: "pilreg is a tool which queries a docker image registry to enumerate images and collect their metadata and filesystems",
	Args:  cobra.MinimumNArgs(1),
	Run:   run,
}

func run(_ *cobra.Command, registries []string) {
	if skiptls {
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	craneoptions := pillage.MakeCraneOptions(insecure)

	if storeImages && resultsPath == "" {
		log.Fatalf("Cannot pull images without destination path. Unset --pull-images or set --results")
	}
	storageOptions := &pillage.StorageOptions{
		StoreImages:  storeImages,
		CachePath:    cachePath,
		ResultsPath:  resultsPath,
		CraneOptions: craneoptions,
	}

	images := pillage.EnumRegistries(registries, repos, tags, craneoptions...)

	var results []*pillage.ImageData
	wg := sizedwaitgroup.New(workerCount)

	for image := range images {
		if resultsPath == "" {
			results = append(results, image)
		} else {
			wg.Add()
			go func(image *pillage.ImageData) {
				image.Store(storageOptions)
				wg.Done()
			}(image)
		}

		if truffleHog == true && CheckTrufflehogInstalled() {
			log.Printf("Running trufflehog against the images...")
			pillage.RunTruffleHog(image)

		}
	}

	wg.Wait()

	if resultsPath == "" {
		out, err := json.Marshal(results)
		if err != nil {
			log.Fatalf("error formatting results for %s: %v", registry, err)
		}
		fmt.Println(string(out))
	}
}

// CheckTrufflehogInstalled verifies if trufflehog is in the system PATH
func CheckTrufflehogInstalled() bool {
	_, err := exec.LookPath("trufflehog")
	if err != nil {
		log.Println("⚠️  trufflehog not found in PATH. Skipping trufflehog scans.")
		return false
	}
	return true
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
