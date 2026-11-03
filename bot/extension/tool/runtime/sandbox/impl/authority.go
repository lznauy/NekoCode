package impl

import "nekocode/bot/extension/tool/runtime/authority"

// Security failures are ordinary errors, never sandbox-unavailability signals.
func protectAuthority(profile Profile) error {
	if profile.Workspace != "" && profile.Mode != ModeReadOnly {
		if err := authority.CheckWritable(profile.Workspace); err != nil {
			return err
		}
	}
	paths, err := resolveWritePaths(profile.WritePaths)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := authority.CheckWritable(path); err != nil {
			return err
		}
	}
	return nil
}
