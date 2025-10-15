package build

import (
	"go.lumeweb.com/portal/build"
)

// Build metadata variables populated at build time via -ldflags.
var (
	// Version is the semantic version of the build
	Version string
	// GitCommit is the Git commit hash
	GitCommit string
	// GitBranch is the Git branch name
	GitBranch string
	// BuildTime is the timestamp when the build was created
	BuildTime string
	// GoVersion is the Go compiler version used
	GoVersion string
	// Platform is the target operating system
	Platform string
	// Architecture is the target CPU architecture
	Architecture string
)

// GetInfo returns build metadata information constructed from the build-time variables.
func GetInfo() build.BuildInfo {
	return build.New(Version, GitCommit, GitBranch, BuildTime, GoVersion, Platform, Architecture)
}
