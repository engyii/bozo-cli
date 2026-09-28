package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"

	"github.com/engyii/bozo-cli/internal/cli"
	"github.com/engyii/bozo-cli/internal/zoho"
)

// version is set by the release build: -ldflags "-X main.version=v1.2.3".
var version = ""

func main() {
	if version == "" {
		version = "dev"
		if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "(devel)" && bi.Main.Version != "" {
			version = bi.Main.Version // set by `go install ...@version`
		}
	}
	zoho.UserAgent = "bozo/" + version

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli.Execute(ctx, version)
	stop()
	os.Exit(code)
}
