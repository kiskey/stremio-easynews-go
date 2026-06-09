package shared

import (
	"os"
	"strconv"
)

// ParseIntEnv safely parses an environment variable as an integer.
// It is guaranteed never to panic. If the variable is missing or malformed,
// it gracefully defaults to the provided fallback value.
func ParseIntEnv(key string, fallback int) int {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return fallback
	}
	return n
}

// ParseBoolEnv safely parses a boolean environment variable.
// It is guaranteed never to panic. It returns true if the value matches "true", "on", or "1".
// It returns false if the value matches "false", "off", or "0".
// Otherwise, it returns the fallback value.
func ParseBoolEnv(key string, fallback bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	switch val {
	case "true", "on", "1":
		return true
	case "false", "off", "0":
		return false
	default:
		return fallback
	}
}

// ParseFloatEnv safely parses a float environment variable with fallback.
// It is guaranteed never to panic. If the variable is empty or not a valid float64, 
// it returns the fallback.
func ParseFloatEnv(key string, fallback float64) float64 {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return fallback
	}
	return f
}
