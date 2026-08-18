package config

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/pefman/ltop/internal/discover"
	"github.com/pefman/ltop/internal/llama"
)

// Setup runs the first-run wizard: it scans localhost, presents any llama.cpp
// servers it found, and persists the chosen endpoint.
func Setup(ctx context.Context, in io.Reader, out io.Writer) (Config, error) {
	fmt.Fprintln(out, "ltop setup")
	fmt.Fprintln(out, "Scanning localhost for llama.cpp servers...")

	scanCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	found := discover.Scan(scanCtx, nil)

	for i, f := range found {
		fmt.Fprintf(out, "  [%d] %s\n", i+1, f.Endpoint)
		fmt.Fprintf(out, "      model %s", orDash(f.Model))
		if f.Build != "" {
			fmt.Fprintf(out, "  build %s", f.Build)
		}
		fmt.Fprintln(out)
		if !f.HasSlots || !f.HasMetric {
			fmt.Fprintf(out, "      note: %s\n", missingNote(f))
		}
	}
	if len(found) == 0 {
		fmt.Fprintln(out, "  none found on the usual ports")
	}

	endpoint, err := prompt(in, out, found)
	if err != nil {
		return Config{}, err
	}

	c := Config{
		Endpoint:       endpoint,
		PollIntervalMS: int(DefaultPollInterval / time.Millisecond),
	}
	if err := Save(c); err != nil {
		return Config{}, fmt.Errorf("save config: %w", err)
	}

	path, _ := Path()
	fmt.Fprintf(out, "\nUsing %s (saved to %s)\n\n", c.Endpoint, path)
	return c, nil
}

func prompt(in io.Reader, out io.Writer, found []discover.Found) (string, error) {
	reader := bufio.NewReader(in)
	for {
		if len(found) > 0 {
			fmt.Fprintf(out, "\nSelect 1-%d, or enter a URL [1]: ", len(found))
		} else {
			fmt.Fprint(out, "\nEnter the server URL: ")
		}

		line, err := reader.ReadString('\n')
		if err != nil && strings.TrimSpace(line) == "" {
			return "", fmt.Errorf("no endpoint selected: %w", err)
		}
		answer := strings.TrimSpace(line)

		if answer == "" && len(found) > 0 {
			return found[0].Endpoint, nil
		}
		if n, convErr := strconv.Atoi(answer); convErr == nil {
			if n >= 1 && n <= len(found) {
				return found[n-1].Endpoint, nil
			}
			fmt.Fprintf(out, "  %d is not in range\n", n)
			continue
		}
		if llama.ValidBase(answer) {
			return llama.NormalizeBase(answer), nil
		}
		fmt.Fprintln(out, "  not a valid URL")
	}
}

func missingNote(f discover.Found) string {
	switch {
	case !f.HasSlots && !f.HasMetric:
		return "server started without --slots and --metrics; ltop will show little"
	case !f.HasSlots:
		return "slot detail unavailable; restart the server with --slots"
	default:
		return "metrics unavailable; restart the server with --metrics"
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
