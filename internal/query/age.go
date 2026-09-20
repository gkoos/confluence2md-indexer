package query

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// ParseAge turns a relative age into a duration. Days and weeks are accepted next to Go
// durations, because "30d" is what an operator reaches for, and the flag and the
// configuration file share this one parser so both accept the same syntax.
func ParseAge(value string) (time.Duration, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, nil
	}

	if amount, unit, ok := splitAmountUnit(trimmed); ok {
		switch unit {
		case "d", "day", "days":
			return time.Duration(amount) * 24 * time.Hour, nil
		case "w", "week", "weeks":
			return time.Duration(amount) * 7 * 24 * time.Hour, nil
		}
	}

	duration, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, errors.New("must be an age such as 30d, 2w or 12h")
	}

	return duration, nil
}

// splitAmountUnit splits a leading integer from a unit suffix ("30d" -> 30 and "d").
func splitAmountUnit(value string) (int, string, bool) {
	digits := 0
	for digits < len(value) && value[digits] >= '0' && value[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits == len(value) {
		return 0, "", false
	}

	amount, err := strconv.Atoi(value[:digits])
	if err != nil {
		return 0, "", false
	}

	return amount, strings.TrimSpace(strings.ToLower(value[digits:])), true
}
