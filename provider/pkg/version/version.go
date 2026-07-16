// Package version provides the provider version, injected at build time via ldflags.
package version

// Version is the current version of the Modal Pulumi provider.
// It is set by the build system via: -ldflags "-X .../version.Version=x.y.z"
var Version string
