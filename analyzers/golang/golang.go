// Package golang implements the analyzer for Go.
//
// A `BuildTarget` in Go is defined as an import path (see `go help importpath`
// for details).
//
// This package is implemented by externally calling both the `go` tool and any
// external build tools.
//
// FAQ
//
// 1. Why not use `go/build`, or a library like `KyleBanks/depth`?
//
// The `go` tool's interface is incredibly stable over different releases, but
// the internals are not. Using these libraries causes crashes when analyzing
// code that is compiled using a different version of Go. (This was how the
// analyzer was originally implemented.)
package golang

import (
	"os"

	"github.com/apex/log"
	"github.com/mitchellh/mapstructure"

	"github.com/fossas/fossa-cli/analyzers/golang/resolver"
	"github.com/fossas/fossa-cli/buildtools/gocmd"
	"github.com/fossas/fossa-cli/exec"
	"github.com/fossas/fossa-cli/module"
)

// An Analyzer contains structs used in the analysis of Go packages. It
// implements analyzer.Analyzer.
type Analyzer struct {
	Go        gocmd.Go
	GoVersion string

	Module  module.Module
	Options Options

	BuildTags []string

	// These caches prevent redundant filesystem lookups and execs, and help a lot
	// when dealing with nested vendoring.
	resolverCache map[string]resolver.Resolver
	projectCache  map[string]Project
}

// Options set analyzer options for Go modules.
type Options struct {
	Tags                      []string `mapstructure:"tags"`                         // specify individual build configurations
	AllTags                   bool     `mapstructure:"all-tags"`                     // Turn on all fossa default build tags
	Strategy                  string   `mapstructure:"strategy"`                     // See the Go analyzer documentation.
	LockfilePath              string   `mapstructure:"lockfile"`                     // For non-standard lockfile locations with strategies `manifest:*`.
	ManifestPath              string   `mapstructure:"manifest"`                     // For non-standard manifest locations with strategies `manifest:*`.
	AllowUnresolved           bool     `mapstructure:"allow-unresolved"`             // Allow unresolved revisions.
	AllowUnresolvedPrefix     string   `mapstructure:"allow-unresolved-prefix"`      // If set, allows unresolved revisions for packages whose import path's prefix matches. Multiple space-delimited prefixes can be specified.
	AllowNestedVendor         bool     `mapstructure:"allow-nested-vendor"`          // Allows vendor folders to be nested and attempts to resolve using parent lockfile lookup.
	AllowDeepVendor           bool     `mapstructure:"allow-deep-vendor"`            // Allows nested vendored dependencies to be resolved using ancestor lockfiles farther than their direct parent.
	AllowExternalVendor       bool     `mapstructure:"allow-external-vendor"`        // Allows reading vendor lockfiles of other projects.
	AllowExternalVendorPrefix string   `mapstructure:"allow-external-vendor-prefix"` // If set, allow reading vendor lockfiles of projects whose import path's prefix matches. Multiple space-delimited prefixes can be specified.
	ModulesVendor             bool     `mapstructure:"modules-vendor"`               // Allows gomodules projects that rely on vendor directories to work.
	SkipImportTracing         bool     `mapstructure:"skip-tracing"`                 // Skips dependency tracing.
	SkipProject               bool     `mapstructure:"skip-project"`                 // Skips project detection.
}

var osTags = []string{"windows", "linux", "freebsd", "android", "darwin", "dragonfly", "nacl", "netbsd", "openbsd", "plan9", "solaris"}
var archTags = []string{"386", "amd64", "amd64p32", "arm", "armbe", "arm64", "arm64be", "ppc64", "ppc64le", "mips", "mipsle", "mips64", "mips64le", "mips64p32", "mips64p32le", "ppc", "s390", "s390x", "sparc", "sparc64"}

// New constructs an Analyzer.
func New(m module.Module) (*Analyzer, error) {
	log.Debugf("%#v", m)

	// Parse and validate options.
	var options Options
	err := mapstructure.Decode(m.Options, &options)
	if err != nil {
		return nil, err
	}
	log.WithField("options", options).Debug("parsed analyzer options")

	// Construct analyzer.
	cmd, version, err := exec.Which("version", os.Getenv("FOSSA_GO_CMD"), "go")
	if err != nil {
		return nil, err
	}

	// Compile a list of all user requested build constraints.
	tags := options.Tags
	if options.AllTags {
		tags = append(tags, osTags...)
		tags = append(tags, archTags...)
	}
	// Include the current build setup.
	tags = append(tags, "")

	return &Analyzer{
		Go: gocmd.Go{
			Cmd: cmd,
			Dir: m.Dir,
		},
		GoVersion: version,

		Module:  m,
		Options: options,

		BuildTags: tags,

		resolverCache: make(map[string]resolver.Resolver),
		projectCache:  make(map[string]Project),
	}, nil
}

// Clean runs `go clean $PKG`.
func (a *Analyzer) Clean() error {
	m := a.Module
	return a.Go.Clean([]string{m.BuildTarget})
}

// Build runs `go build $PKG`.
func (a *Analyzer) Build() error {
	m := a.Module
	return a.Go.Build([]string{m.BuildTarget})
}

// IsBuilt runs `go list $PKG` and checks for errors.
func (a *Analyzer) IsBuilt() (bool, error) {
	m := a.Module
	log.Debugf("%#v", m)
	pkg, err := a.Go.ListOne(m.BuildTarget, nil, a.Options.ModulesVendor)
	if err != nil {
		return false, err
	}
	return pkg.Error == nil, nil
}
