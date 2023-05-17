package logging

import (
	"bytes"
	"context"
	"fmt"

	"github.com/gookit/color"
	"github.com/werf/logboek"
)

var (
	imageNameFormat     = "⛵ image %s"
	artifactNameFormat  = "🛸 artifact %s"
	imageMetadataFormat = "⚙ image %s metadata"
)

func ImageLogName(name string, isArtifact bool) string {
	if !isArtifact {
		if name == "" {
			name = "~"
		}
	}

	return name
}

func ImageMetadataLogProcess(name string) string {
	return fmt.Sprintf(imageMetadataFormat, name)
}

func ImageLogProcessName(name string, isArtifact bool, targetPlatform string) string {
	appendPlatformFunc := func(name string) string {
		if targetPlatform == "" {
			return name
		}
		return fmt.Sprintf("%s [%s]", name, targetPlatform)
	}

	logName := ImageLogName(name, isArtifact)

	if !isArtifact {
		return appendPlatformFunc(fmt.Sprintf(imageNameFormat, logName))
	} else {
		return appendPlatformFunc(fmt.Sprintf(artifactNameFormat, logName))
	}
}

func DisablePrettyLog() {
	imageNameFormat = "image %s"
	imageMetadataFormat = "image %s metadata"
	artifactNameFormat = "artifact %s"
}

func ImageDefaultStyle(isArtifact bool) color.Style {
	var colors []color.Color
	if isArtifact {
		colors = []color.Color{color.FgCyan, color.Bold}
	} else {
		colors = []color.Color{color.FgYellow, color.Bold}
	}

	return color.New(colors...)
}

func ImageMetadataStyle() color.Style {
	return ImageDefaultStyle(false)
}

// RunWithDeferredLog will run a function and print log if condition is true and
// print logs only on error if condition is false.
func RunWithDeferredLog(ctx context.Context, deferLog bool, run func(ctx context.Context) error) error {
	if !deferLog {
		return run(ctx)
	}

	buf := new(bytes.Buffer)
	bufLogger := logboek.NewLogger(buf, buf)
	ctxWithBufLogger := logboek.NewContext(ctx, bufLogger)

	err := run(ctxWithBufLogger)
	if err != nil {
		fmt.Println(buf.String())
	}
	return err
}
