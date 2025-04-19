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
)

func init() {
	// Registry config options
	scanFlags := pflag.NewFlagSet("Scan Options", pflag.ContinueOnError)
	scanFlags.StringSliceVarP(&repos, "repos", "r", []string{}, "List of repositories to scan. If blank, uses the registry's catalog API.")
	scanFlags.StringSliceVarP(&tags, "tags", "t", []string{}, "List of tags to scan per repository. If blank, uses the tags API.")
	rootCmd.PersistentFlags().AddFlagSet(scanFlags)

	// Storage config options
	storageFlags := pflag.NewFlagSet("Storage Options", pflag.ContinueOnError)
	storageFlags.StringVarP(&outputPath, "output", "o", ".", "Directory to store output. Required with --store-images.")
	storageFlags.BoolVarP(&storeImages, "store-images", "s", false, "Download and store image filesystems.")
	storageFlags.StringVarP(&cachePath, "cache", "c", ".", "Path to cache image layers.")
	storageFlags.Int64VarP(&filterSmall, "small", "f", 40000, "Filter analysis on layers that are this size in bytes. (Default 40k)")
	rootCmd.PersistentFlags().AddFlagSet(storageFlags)

	// Analysis config options
	analysisFlags := pflag.NewFlagSet("Analysis Options", pflag.ContinueOnError)
	analysisFlags.BoolVarP(&truffleHog, "trufflehog", "x", false, "Scan image contents with TruffleHog.")
	analysisFlags.BoolVarP(&whiteOut, "whiteout", "0", false, "Look for deleted/whiteout files in image layers.")
	rootCmd.PersistentFlags().AddFlagSet(analysisFlags)

	// Connection options
	connFlags := pflag.NewFlagSet("Connection/Runtime Options", pflag.ContinueOnError)
	connFlags.BoolVarP(&skiptls, "skip-tls", "k", false, "Disable TLS verification.")
	connFlags.BoolVarP(&insecure, "insecure", "i", false, "Use HTTP instead of HTTPS.")
	connFlags.IntVarP(&workerCount, "workers", "w", 8, "Number of concurrent workers.")
	rootCmd.PersistentFlags().AddFlagSet(connFlags)
}

var rootCmd = &cobra.Command{
	Use:   "pilreg <registry>",
	Short: "pilreg is a tool which queries a docker image registry to enumerate images and collect their metadata and filesystems",
	Args:  cobra.MinimumNArgs(1),
	Run:   run,
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

func run(_ *cobra.Command, registries []string) {
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

	images := pillage.EnumRegistries(registries, repos, tags, craneoptions...)

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
		fmt.Println("\n Scan:")
		printFlags(cmd, []string{"repos", "tags"})

		fmt.Println("\n Storage:")
		printFlags(cmd, []string{"results", "store-images", "cache"})

		fmt.Println("\n Secret Hunting:(assumes storage above)")
		printFlags(cmd, []string{"trufflehog", "whiteout"})

		fmt.Println("\nOther options:")
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			if !contains([]string{"repos", "tags", "results", "store-images", "cache", "trufflehog", "whiteout"}, f.Name) {
				fmt.Printf("  --%s	%s\n", f.Name, f.Usage)
			}
		})
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

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
