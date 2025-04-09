package utils

import (
	"bufio"
	"context"
	"fmt"
	"io"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
)

func RedirectVmmOutput(ctx context.Context, tag string, output io.ReadCloser) {
	writer := telemetry.NewEventWriter(ctx, tag)

	defer func() {
		readerErr := output.Close()
		if readerErr != nil {
			errMsg := fmt.Errorf("error closing redirect reader for %s: %w", tag, readerErr)
			telemetry.ReportError(ctx, errMsg)
		}
	}()
	scanner := bufio.NewScanner(output)

	for scanner.Scan() {
		line := scanner.Text()
		writer.Write([]byte(line))
	}

	readerErr := scanner.Err()
	if readerErr != nil {
		errMsg := fmt.Errorf("error reading %s: %w", tag, readerErr)
		telemetry.ReportError(ctx, errMsg)
		writer.Write([]byte(errMsg.Error()))
	} else {
		telemetry.ReportEvent(ctx, fmt.Sprintf("%s finish", tag))
	}
}
