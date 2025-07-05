package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/remeh/sizedwaitgroup"

	"github.com/antitree/go-pillage-registries/pkg/pillage"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	repos       []string
	tags        []string
	localTar    string
	skiptls     bool
	insecure    bool
	storeImages bool
	registry    string
	cachePath   string
	outputPath  string
	workerCount int
	truffleHog  bool
	whiteOut    bool
	filterSmall int64
	showVersion bool
	debug       bool
)

var (
	version   = "dev"
	buildDate = "unknown"
)

func init() {
	// Registry config options
	scanFlags := pflag.NewFlagSet("Registry Options", pflag.ContinueOnError)
	scanFlags.StringSliceVarP(&repos, "repos", "r", []string{}, "List of repositories to scan. If blank, uses the registry's catalog API.")
	scanFlags.StringSliceVarP(&tags, "tags", "t", []string{}, "List of tags to scan per repository. If blank, uses the tags API.")
	scanFlags.StringVarP(&localTar, "local", "l", "", "Path to a local image tarball to scan.")
	rootCmd.PersistentFlags().AddFlagSet(scanFlags)

	// Storage config options
	storageFlags := pflag.NewFlagSet("Storage Options", pflag.ContinueOnError)
	storageFlags.StringVarP(&outputPath, "output", "o", ".", "Directory to store output. Required with --store-images.(./results/ by default)")
	storageFlags.BoolVarP(&storeImages, "store-images", "s", false, "Download and store image filesystems.")
	storageFlags.StringVarP(&cachePath, "cache", "c", ".", "Path to cache image layers. (/tmp by default)")
	rootCmd.PersistentFlags().AddFlagSet(storageFlags)

	// Analysis config options
	analysisFlags := pflag.NewFlagSet("Analysis Options", pflag.ContinueOnError)
	analysisFlags.BoolVarP(&truffleHog, "trufflehog", "x", false, "Scan image contents with TruffleHog.")
	analysisFlags.BoolVarP(&whiteOut, "whiteout", "w", false, "Look for deleted/whiteout files in image layers.")

	var all bool
	analysisFlags.BoolVarP(&all, "all", "a", false, "Enable all analysis options by default. (Very noisy!)")
	rootCmd.PersistentFlags().AddFlagSet(analysisFlags)

	if all {
		truffleHog = true
		whiteOut = true
	}

	// Connection options
	connFlags := pflag.NewFlagSet("Connection Options", pflag.ContinueOnError)
	connFlags.BoolVarP(&skiptls, "skip-tls", "k", false, "Disable TLS verification.")
	connFlags.BoolVarP(&insecure, "insecure", "i", false, "Use HTTP instead of HTTPS.")
	connFlags.IntVar(&workerCount, "workers", 8, "Number of concurrent workers.")
	connFlags.BoolVar(&showVersion, "version", false, "Print version information and exit.")
	connFlags.BoolVar(&debug, "debug", false, "Enable debug logging.")
	rootCmd.PersistentFlags().AddFlagSet(connFlags)
}

var rootCmd = &cobra.Command{
	Use:     "pilreg <registry>",
	Short:   "pilreg is a tool which queries a docker image registry to enumerate images and collect their metadata and filesystems",
	Args:    cobra.ArbitraryArgs,
	Run:     run,
	Version: version + " (" + buildDate + ")",
}

// NormalizeFlags applies implicit behavior for CLI combinations.
func NormalizeFlags() {
	if cachePath != "" && !storeImages {
		storeImages = true
	}
	if truffleHog && !storeImages {
		storeImages = true
	}

	for i, repo := range repos {
		//name := repo
		//tag := "latest" // default fallback

		if strings.Contains(repo, ":") {
			parts := strings.SplitN(repo, ":", 2)
			repos[i] = parts[0]
			tags = append(tags, parts[1])
		}
	}

	if whiteOut && outputPath == "." {
		log.Println("⚠️  --whiteout was set without --output or -o. Layers will be processed in memory.")
	}
	if storeImages && outputPath == "." {
		outputPath = "."
		pillage.LogWarn("--store-images requires output. Setting it to the current directory")
	}
}

func run(cmd *cobra.Command, registries []string) {
	if showVersion {
		fmt.Printf("pilreg %s (%s)\n", version, buildDate)
		return
	}
	if localTar == "" && len(registries) > 0 && isTarballPath(registries[0]) {
		localTar = registries[0]
		registries = registries[1:]
	}
	if len(registries) == 0 && localTar == "" {
		cmd.Help()
		return
	}

	NormalizeFlags()

	if skiptls {
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	craneoptions := pillage.MakeCraneOptions(insecure)

	storageOptions := &pillage.StorageOptions{
		StoreImages:  storeImages,
		CachePath:    cachePath,
		OutputPath:   outputPath,
		CraneOptions: craneoptions,
		WhiteOut:     whiteOut,
		FilterSmall:  filterSmall,
	}

	var images <-chan *pillage.ImageData
	if localTar != "" {
		if err := pillage.ValidateTarball(localTar); err != nil {
			log.Fatalf("invalid tarball %s: %v", localTar, err)
		}
		images = pillage.EnumTarball(localTar)
	} else {
		images = pillage.EnumRegistries(registries, repos, tags, craneoptions...)
	}

	var results []*pillage.ImageData
	wg := sizedwaitgroup.New(workerCount)

	for image := range images {

		if outputPath == "." && !whiteOut {
			results = append(results, image)
		} else {
			wg.Add()
			go func(image *pillage.ImageData) {
				image.Store(storageOptions)
				wg.Done()
			}(image)
		}

		if truffleHog && CheckTrufflehogInstalled() {
			go pillage.RunTruffleHog(image)
		}

	}

	wg.Wait()

	if outputPath == "" {
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

// SetHelpFunc prints grouped help output for categorized flags
func init() {
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		fmt.Print("Usage: pilreg [OPTIONS] <registry>\n\n")
		fmt.Println("pilreg is penetration testing tool targeting container images hosted in a registry or in a tar ball.\n")

		fmt.Println("Examples:\n")
		fmt.Println("  pilreg 127.0.0.1:5000 -a")
		fmt.Println("  pilreg 127.0.0.1:5000 --repos nginx --tags latest,stable")
		fmt.Println("  pilreg gcr.io --repos <project>/<my image>:latest")
		fmt.Println("  pilreg --local <path/to/tarball.tar> --whiteout")
		fmt.Println("  pilreg <registry> --trufflehog")

		fmt.Println("\n Registry/Local config options:")
		printFlags(cmd, []string{"repos", "tags", "local"})

		fmt.Println("\n Storage config options:")
		printFlags(cmd, []string{"output", "store-images", "cache", "small"})

		fmt.Println("\n Analysis config options:")
		printFlags(cmd, []string{"trufflehog", "whiteout"})

		fmt.Println("\n Connection options:")
		printFlags(cmd, []string{"skip-tls", "insecure", "workers"})

		fmt.Println("")
		printFlags(cmd, []string{"version"})
		printFlags(cmd, []string{"debug"})
	})
}

func printFlags(cmd *cobra.Command, names []string) {
	for _, name := range names {
		flag := cmd.Flag(name)
		if flag != nil {
			fmt.Printf("  --%s	%s\n", flag.Name, flag.Usage)
		}
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func isTarballPath(path string) bool {
	tarExts := []string{".tar", ".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tar.xz", ".txz"}
	lower := strings.ToLower(path)
	for _, ext := range tarExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
