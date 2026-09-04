package main

// Version is the derpy semantic version string, injected at build time
// by goreleaser via ldflags (-X main.Version={{.Version}}).
// The default "dev" value is used for developer builds (`go build` without
// ldflags) and printed by the `derpy version` subcommand.
var Version = "dev"
