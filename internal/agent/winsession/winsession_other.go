//go:build !windows

package winsession

// ActiveUser solo tiene sentido en Windows.
func ActiveUser() string { return "" }

// ActiveUsers solo tiene sentido en Windows.
func ActiveUsers() []string { return nil }
