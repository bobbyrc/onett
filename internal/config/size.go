package config

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ParseSize reads a byte count such as "25MB", "512k", "1.5GB" or "1048576". The units B, K, M and
// G (with an optional trailing B) are powers of 1024. The result must be a whole number of bytes.
func ParseSize(s string) (Size, error) {
	orig := s
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, "B")
	mult := float64(1)
	for suffix, m := range map[string]float64{"K": 1 << 10, "M": 1 << 20, "G": 1 << 30} {
		if rest, ok := strings.CutSuffix(s, suffix); ok {
			s, mult = rest, m
			break
		}
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n*mult > math.MaxInt64/2 {
		return 0, fmt.Errorf("invalid size %q: want a number of bytes or a value such as \"25MB\"", orig)
	}
	if b := n * mult; b != math.Trunc(b) {
		return 0, fmt.Errorf("invalid size %q: not a whole number of bytes", orig)
	}
	return Size(n * mult), nil
}

// String writes the size with the largest unit that divides it exactly: "25MB", "1536KB", "100B".
func (s Size) String() string {
	for _, u := range []struct {
		name string
		n    Size
	}{{"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}} {
		if s >= u.n && s%u.n == 0 {
			return fmt.Sprintf("%d%s", s/u.n, u.name)
		}
	}
	return fmt.Sprintf("%dB", int64(s))
}

// UnmarshalYAML accepts a number of bytes or a string such as "25MB".
func (s *Size) UnmarshalYAML(node *yaml.Node) error {
	var raw string
	if err := node.Decode(&raw); err != nil {
		return fmt.Errorf("size: want a number of bytes or a string such as \"25MB\": %w", err)
	}
	v, err := ParseSize(raw)
	if err != nil {
		return err
	}
	*s = v
	return nil
}

// MarshalYAML writes the size as a string such as "25MB".
func (s Size) MarshalYAML() (any, error) { return s.String(), nil }

// MarshalJSON writes the size as a string such as "25MB".
func (s Size) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }

// UnmarshalJSON accepts a string such as "25MB" or a number of bytes.
func (s *Size) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		var n int64
		if err := json.Unmarshal(data, &n); err != nil {
			return fmt.Errorf("size: want a number of bytes or a string such as \"25MB\": %w", err)
		}
		*s = Size(n)
		return nil
	}
	v, err := ParseSize(raw)
	if err != nil {
		return err
	}
	*s = v
	return nil
}
