package version

import "github.com/abcxyz/pkg/buildinfo"

var (
	// Name is the name of the binary. This can be overridden by the build
	// process.
	Name = "ar-terraform-registry"

	// Version is the main package version. This can be overridden by the build
	// process.
	Version = buildinfo.Version()

	// Commit is the git sha. This can be overridden by the build process.
	Commit = buildinfo.Commit()

	// OSArch is the operating system and architecture combination.
	OSArch = buildinfo.OSArch()

	// HumanVersion is the compiled version.
	HumanVersion = Name + " " + Version + " (" + Commit + ", " + OSArch + ")"
)
