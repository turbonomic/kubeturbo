package configs

import (
	"testing"
)

func TestGetNewConfigValue(t *testing.T) {
	getKeyStringVal := func(key string) string {
		switch key {
		case "empty":
			return ""
		case "negative":
			return "-10"
		case "invalid":
			return "abc"
		case "good":
			return "42"
		default:
			return ""
		}
	}

	defaultValue := 10

	tests := []struct {
		configKey      string
		expectedResult int
	}{
		{"empty", defaultValue},       // Empty string should return default value
		{"negative", defaultValue},    // Negative number should return default value
		{"invalid", defaultValue},     // Invalid input should return default value
		{"good", 42},                  // Good number should return the parsed value
		{"nonexistent", defaultValue}, // Nonexistent key should return default value
	}

	for _, test := range tests {
		result := GetNodePoolSizeConfigValue(test.configKey, getKeyStringVal, defaultValue)
		if result != test.expectedResult {
			t.Errorf("For key '%s', expected: %d, but got: %d", test.configKey, test.expectedResult, result)
		}
	}
}
