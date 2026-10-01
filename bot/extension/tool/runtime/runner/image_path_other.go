//go:build !windows

package runner

func validateLocalImagePath(string) error { return nil }
