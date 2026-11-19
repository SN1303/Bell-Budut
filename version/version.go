// Package version holds the application version and update source.
package version

// Version is overwritten at build time by the release workflow:
//
//	go build -ldflags "-X bell-budut/version.Version=4.6.0"
var Version = "4.7.1"

// Repo is the GitHub repository used for OTA updates (GitHub Releases).
const Repo = "SN1303/Bell-Budut"

// AssetName is the release asset that contains bell-budut.exe and the web/ folder.
const AssetName = "bell-budut-windows-amd64.zip"
