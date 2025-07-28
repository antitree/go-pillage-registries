package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"

	"github.com/remeh/sizedwaitgroup"

	"github.com/antitree/go-pillage-registries/pkg/pillage"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	repos          []string
	tags           []string
	localTar       string
	skiptls        bool
	insecure       bool
	storeImages    bool
	registry       string
	cachePath      string
	outputPath     string
	workerCount    int
	truffleHog     bool
	whiteOut       bool
	whiteOutFilter []string
	filterSmall    int64
	showVersion    bool
	debug          bool
	all            bool   // Enable all analysis options by default
	token          string // Bearer token or password for auth
	username       string // Optional username when using token
	hashIndex      *pillage.HashIndex
)

var (
	version      = "2.0"
	buildDate    = "unknown"
	autocomplete string // shell type for generating completion script
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
	analysisFlags.StringSliceVar(&whiteOutFilter, "whiteout-filter", nil, "Filter patterns when extracting whiteouts. Defaults to 'tmp,cache,apk,apt'.")
	analysisFlags.Lookup("whiteout-filter").NoOptDefVal = "tmp,cache,apk,apt,downloaded_packages,dist-info,site-packages,mssql-tools/bin,*/tmp/downloaded_packages/**,*/wheels/**,*/site-packages/**,*/.dist-info/**,*/opt/*-tmp/**,*/usr/share/info/**,*/mssql-tools/bin/**"
	analysisFlags.BoolVarP(&all, "all", "a", true, "Enable all analysis options by default. (Very noisy!)")

	rootCmd.PersistentFlags().AddFlagSet(analysisFlags)

	// Connection options
	connFlags := pflag.NewFlagSet("Connection Options", pflag.ContinueOnError)
	connFlags.BoolVarP(&skiptls, "skip-tls", "k", false, "Disable TLS verification.")
	connFlags.BoolVarP(&insecure, "insecure", "i", false, "Use HTTP instead of HTTPS.")
	connFlags.StringVar(&token, "token", "", "Registry bearer token or password")
	connFlags.StringVar(&username, "username", "", "Username for token auth (default 'pilreg' if omitted)")
	connFlags.IntVar(&workerCount, "workers", 8, "Number of concurrent workers.")
	connFlags.BoolVar(&showVersion, "version", false, "Print version information and exit.")
	connFlags.BoolVarP(&debug, "debug", "d", false, "Enable debug logging.")
	rootCmd.PersistentFlags().AddFlagSet(connFlags)
	// Autocomplete script generation (bash|zsh|fish|powershell)
	rootCmd.PersistentFlags().StringVar(&autocomplete, "autocomplete", "", "Generate shell completion script for specified shell (bash|zsh|fish|powershell)")
	// hide internal use
	_ = rootCmd.PersistentFlags().MarkHidden("autocomplete")
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
	if len(whiteOutFilter) > 0 {
		whiteOut = true
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
	pillage.SetDebug(debug)

	if showVersion {
		fmt.Printf("pilreg %s (%s)\n", version, buildDate)
		return
	}

	if all && !truffleHog && !whiteOut {
		truffleHog = true
		whiteOut = true
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

	indexFile := filepath.Join(outputPath, "scanned_shas.log")
	var err error
	hashIndex, err = pillage.NewHashIndex(indexFile)
	if err != nil {
		log.Fatalf("failed to init hash index: %v", err)
	}

	if skiptls {
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	var auth authn.Authenticator
	if token != "" {
		if username == "" {
			username = "pilreg"
			log.Println("⚠️  --token provided without --username; using 'pilreg'. Some registries require a username.")
		}
		auth = authn.FromConfig(authn.AuthConfig{Username: username, Password: token})
	} else {
		log.Println("ℹ️  no token provided; using local Docker credentials if available")
		for _, r := range registries {
			reg, err := name.NewRegistry(r)
			if err != nil {
				log.Printf("   unable to parse registry %s: %v", r, err)
				continue
			}
			a, err := authn.DefaultKeychain.Resolve(reg)
			if err != nil {
				log.Printf("   no credentials found for %s", r)
				continue
			}
			cfg, err := a.Authorization()
			if err != nil {
				log.Printf("   failed to get credentials for %s: %v", r, err)
				continue
			}
			snip := pillage.CredentialSnippet(cfg)
			if snip == "anonymous" {
				log.Printf("   using anonymous access for %s", r)
			} else {
				log.Printf("   using credentials for %s (%s)", r, snip)
			}
		}
	}

	craneoptions := pillage.MakeCraneOptions(insecure, auth)

	storageOptions := &pillage.StorageOptions{
		StoreImages:    storeImages,
		CachePath:      cachePath,
		OutputPath:     outputPath,
		CraneOptions:   craneoptions,
		WhiteOut:       whiteOut,
		WhiteOutFilter: whiteOutFilter,
		FilterSmall:    filterSmall,
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

		hash := pillage.ImageHash(image)
		exists, err := hashIndex.AddIfMissing(hash)
		if err != nil {
			log.Printf("failed recording hash: %v", err)
		}
		if exists {
			pillage.LogInfo("Skipping already scanned image %s", image.Reference)
			continue
		}

		if outputPath == "." && !whiteOut {
			results = append(results, image)
		} else {
			wg.Add()
			go func(img *pillage.ImageData) {
				img.Store(storageOptions)
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
		fmt.Println("pilreg is penetration testing tool targeting container images hosted in a registry or in a tar ball.")

		fmt.Println("Examples:")
		fmt.Println("  pilreg 127.0.0.1:5000 -a")
		fmt.Println("  pilreg <registry> --repos nginx --tags latest,stable")
		fmt.Println("  pilreg <registry> --repos test/nginx:latest")
		fmt.Println("  pilreg ghcr.io --repos <gh username>/<repo>/<package/image> --username --token <PAT> -a")
		fmt.Println("  pilreg --local <path/to/tarball.tar> --whiteout")
		fmt.Println("  pilreg --local <path/to/tarball.tar> --whiteout-filter=apk,tmp,test")
		fmt.Println("  pilreg <registry> --trufflehog")

		fmt.Println("\n Registry/Local config options:")
		printFlags(cmd, []string{"repos", "tags", "local"})

		fmt.Println("\n Storage config options:")
		printFlags(cmd, []string{"output", "store-images", "cache", "small"})

		fmt.Println("\n Analysis config options:")
		printFlags(cmd, []string{"trufflehog", "whiteout", "whiteout-filter"})

		fmt.Println("\n Connection options:")
		printFlags(cmd, []string{"skip-tls", "insecure", "token", "username", "workers"})

		fmt.Println("")
		printFlags(cmd, []string{"version"})
		printFlags(cmd, []string{"debug"})
		// Autocomplete flag (hidden in default listing)
		printFlags(cmd, []string{"autocomplete"})
	})
	// Autocomplete handling before running any command
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if autocomplete != "" {
			switch autocomplete {
			case "bash":
				_ = rootCmd.GenBashCompletion(os.Stdout)
			case "zsh":
				_ = rootCmd.GenZshCompletion(os.Stdout)
			case "fish":
				_ = rootCmd.GenFishCompletion(os.Stdout, true)
			case "powershell":
				_ = rootCmd.GenPowerShellCompletion(os.Stdout)
			default:
				fmt.Fprintf(os.Stderr, "Unsupported shell for autocomplete: %s\n", autocomplete)
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
}

func printFlags(cmd *cobra.Command, names []string) {
	const padWidth = 24
	for _, name := range names {
		if flag := cmd.Flag(name); flag != nil {
			// build combined flag string
			var flagText string
			if flag.Shorthand != "" {
				flagText = fmt.Sprintf("-%s, --%s", flag.Shorthand, flag.Name)
			} else {
				flagText = fmt.Sprintf("    --%s", flag.Name)
			}
			// align descriptions in a single column
			fmt.Printf("  %-*s %s\n", padWidth, flagText, flag.Usage)
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
