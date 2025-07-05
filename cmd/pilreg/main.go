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
	"github.com/antitree/go-pillage-registries/pkg/version"
	"github.com/spf13/cobra"
	flag "github.com/spf13/pflag"
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
	whiteout    bool
	versionFlag bool
)

func init() {
	rootCmd.Version = version.Get()

	rootCmd.Flags().SortFlags = false

	// Registry config options
	registryFlags := flag.NewFlagSet("registry", flag.ExitOnError)
	registryFlags.SortFlags = false
	registryFlags.StringSliceVarP(&repos, "repos", "r", []string{}, "list of repositories to scan on the registry. If blank, pilreg will attempt to enumerate them using the catalog API")
	registryFlags.StringSliceVarP(&tags, "tags", "t", []string{}, "list of tags to scan on each repository. If blank, pilreg will attempt to enumerate them using the tags API")
	rootCmd.PersistentFlags().AddFlagSet(registryFlags)

	// Storage config options
	storageFlags := flag.NewFlagSet("storage", flag.ExitOnError)
	storageFlags.SortFlags = false
	storageFlags.StringVarP(&resultsPath, "results", "o", "", "Path to directory for storing results. If blank, outputs configs and manifests as json object to Stdout.(must be used if 'store-images' is enabled)")
	storageFlags.BoolVarP(&storeImages, "store-images", "s", false, "Downloads filesystem for discovered images and stores an archive in the output directory (Disabled by default, requires --results to be set)")
	storageFlags.StringVarP(&cachePath, "cache", "c", "", "Path to cache image layers (optional, only used if images are pulled)")
	storageFlags.BoolVarP(&whiteout, "whiteout", "w", false, "Include whiteout files when saving filesystem archive")
	rootCmd.PersistentFlags().AddFlagSet(storageFlags)

	// Analysis config options
	analysisFlags := flag.NewFlagSet("analysis", flag.ExitOnError)
	analysisFlags.SortFlags = false
	analysisFlags.BoolVarP(&truffleHog, "trufflehog", "x", false, "Integrate with Trufflehog to scan the images once they are found")
	rootCmd.PersistentFlags().AddFlagSet(analysisFlags)

	// Connection options
	connectionFlags := flag.NewFlagSet("connection", flag.ExitOnError)
	connectionFlags.SortFlags = false
	connectionFlags.BoolVarP(&skiptls, "skip-tls", "k", false, "Disables TLS certificate verification")
	connectionFlags.BoolVarP(&insecure, "insecure", "i", false, "Fetch Data over plaintext")
	connectionFlags.IntVar(&workerCount, "workers", 8, "Number of workers when pulling images. If set too high, this may cause errors. (optional, only used if images are pulled)")
	connectionFlags.BoolVar(&versionFlag, "version", false, "Print version and exit")
	rootCmd.PersistentFlags().AddFlagSet(connectionFlags)

	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(cmd.OutOrStdout(), cmd.Short)
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintf(cmd.OutOrStdout(), "Usage:\n  %s\n\n", cmd.UseLine())

		if registryFlags.HasAvailableFlags() {
			fmt.Fprintln(cmd.OutOrStdout(), "Registry config options:")
			fmt.Fprint(cmd.OutOrStdout(), registryFlags.FlagUsages())
			fmt.Fprintln(cmd.OutOrStdout())
		}

		if storageFlags.HasAvailableFlags() {
			fmt.Fprintln(cmd.OutOrStdout(), "Storage config options:")
			fmt.Fprint(cmd.OutOrStdout(), storageFlags.FlagUsages())
			fmt.Fprintln(cmd.OutOrStdout())
		}

		if analysisFlags.HasAvailableFlags() {
			fmt.Fprintln(cmd.OutOrStdout(), "Analysis config options:")
			fmt.Fprint(cmd.OutOrStdout(), analysisFlags.FlagUsages())
			fmt.Fprintln(cmd.OutOrStdout())
		}

		if connectionFlags.HasAvailableFlags() {
			fmt.Fprintln(cmd.OutOrStdout(), "Connection options:")
			fmt.Fprint(cmd.OutOrStdout(), connectionFlags.FlagUsages())
		}
	})
}

var rootCmd = &cobra.Command{
	Use:   "pilreg <registry>",
	Short: "pilreg is a tool which queries a docker image registry to enumerate images and collect their metadata and filesystems",
	Args:  cobra.MinimumNArgs(1),
	Run:   run,
}

func run(_ *cobra.Command, registries []string) {
	if versionFlag {
		fmt.Println(version.Get())
		return
	}
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
		Whiteout:     whiteout,
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
