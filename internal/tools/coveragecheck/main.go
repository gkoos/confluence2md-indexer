package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	profile := flag.String("profile", "coverage.out", "path to go coverage profile")
	min := flag.Float64("min", minFromEnv(), "minimum total coverage percentage")
	flag.Parse()

	total, covered, err := parseProfile(*profile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "coverage check failed: %v\n", err)
		os.Exit(1)
	}
	if total == 0 {
		fmt.Fprintln(os.Stderr, "coverage check failed: profile contains no statements")
		os.Exit(1)
	}

	pct := (float64(covered) / float64(total)) * 100
	fmt.Printf("Total coverage: %.1f%% (minimum: %.1f%%)\n", pct, *min)
	if pct < *min {
		fmt.Fprintf(os.Stderr, "Coverage gate failed: %.1f%% < %.1f%%\n", pct, *min)
		os.Exit(1)
	}
}

func minFromEnv() float64 {
	raw := strings.TrimSpace(os.Getenv("COVERAGE_MIN"))
	if raw == "" {
		return 70
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v < 0 || v > 100 {
		return 70
	}
	return v
}

func parseProfile(path string) (total int64, covered int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = f.Close() }()

	s := bufio.NewScanner(f)
	lineNo := 0
	for s.Scan() {
		lineNo++
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		if lineNo == 1 {
			if !strings.HasPrefix(line, "mode:") {
				return 0, 0, fmt.Errorf("invalid profile header: %q", line)
			}
			continue
		}

		fields := strings.Fields(line)
		if len(fields) != 3 {
			return 0, 0, fmt.Errorf("invalid profile line %d: %q", lineNo, line)
		}

		stmts, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parse stmt count at line %d: %w", lineNo, err)
		}
		count, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parse hit count at line %d: %w", lineNo, err)
		}

		total += stmts
		if count > 0 {
			covered += stmts
		}
	}
	if err := s.Err(); err != nil {
		return 0, 0, err
	}

	return total, covered, nil
}
