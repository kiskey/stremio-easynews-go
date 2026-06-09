package shared

import (
	"os"
	"strconv"
)

// ParseIntEnv parses an environment variable as an integer.
// If the variable is empty or not a valid integer, it returns the fallback.
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

// ParseBoolEnv parses a boolean environment variable.
// Returns true if the value matches "true", "on", or "1".
// Returns false if the value matches "false", "off", or "0".
// Otherwise, returns the fallback value.
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

// ParseFloatEnv parses a float environment variable with fallback.
// If the variable is empty or not a valid float64, it returns the fallback.
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
