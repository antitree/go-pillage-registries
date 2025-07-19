# go-pillage-registries

![go-pillage-registries logo](images/logo-small.png)

This project takes a Docker registry and pillages the manifest and configuration for each image in its catalog.

It uses Google's [crane](https://github.com/google/go-containerregistry/blob/master/cmd/crane/doc/crane.md) command's package, which should follow docker's keychain semantics.
If you would like to override this, just change `authn.DefaultKeychain` as described in the <https://github.com/google/go-containerregistry/tree/master/pkg/authn/k8schain>

## Install:

```bash
git clone https://github.com/nccgroup/go-pillage-registries.git

cd go-pillage-registries
go install ./...

```

## Usage:

```
Usage: pilreg <registry> | -l <tarbalpath> [OPTIONS]

pilreg is penetration testing tool targeting container images hosted in a registry or in a tar ball.
Examples:
  pilreg 127.0.0.1:5000 -a
  pilreg 127.0.0.1:5000 --repos nginx --tags latest,stable
  pilreg <registry> --repos <project>/<my image>:latest
  pilreg --local <path/to/tarball.tar> --whiteout
  pilreg --local <path/to/tarball.tar> --whiteout-filter=apk,tmp,test
  pilreg 
  pilreg <registry> --trufflehog

 Registry/Local config options:
  --repos	List of repositories to scan. If blank, uses the registry's catalog API.
  --tags	List of tags to scan per repository. If blank, uses the tags API.
  --local	Path to a local image tarball to scan.

 Storage config options:
  --output	Directory to store output. Required with --store-images.(./results/ by default)
  --store-images	Download and store image filesystems.
  --cache	Path to cache image layers. (/tmp by default)

 Analysis config options:
  --trufflehog	Scan image contents with TruffleHog.
  --whiteout	Look for deleted/whiteout files in image layers.
  --whiteout-filter     Filter patterns when extracting whiteouts. Defaults to 'tmp,cache,apk,apt'.

 Connection options:
  --skip-tls	Disable TLS verification.
  --insecure	Use HTTP instead of HTTPS.
  --token       Registry bearer token or password. If omitted, pilreg uses
                credentials from your local Docker configuration and logs
                the registry and a snippet of the credential in use.
  --username	Username for token auth
  --workers	Number of concurrent workers.

  --version	Print version information and exit.
  --debug	Enable debug logging.
```
If `--local` is not provided and the value for `<registry>` ends with a common tarball extension such as `.tar`, `.tar.gz`, or `.tgz`, `pilreg` will automatically switch to local mode and scan that file.

When pilreg completes a scan it saves the Docker image history in a file named with the image's sha256 digest under `<output>/results/`. If this file is found on a subsequent run, the image is skipped and not scanned again.

## Example:

In the [example directory](example/) there is an example of an image which
Docker image that is a server that has a secret.

## Acknowledgments
* Thanks to @jmakinen-ncc the original author of NCC Group's go-registry-pillage
* @jonjohnsonjr: For the idea around the whiteout file feature (and writing Crane)
