//go:build !windows

package tlscert

/*
 * Everywhere that is not Windows, nothing is classified.
 *
 * Linux keeps temporary addresses too, and `IFA_F_TEMPORARY` says so — but
 * reading it means a netlink conversation, and LANcast's federation is a
 * Windows story today. An empty set means `StableIPs` keeps everything, which
 * is the same answer it gave before this existed.
 *
 * The split is here rather than behind a runtime check so the Windows call is
 * absent from a Linux binary entirely, which is also why `GOOS=linux go vet`
 * is run before anything touching a `_windows.go` file is pushed.
 */
func temporaryAddresses() map[string]bool { return nil }
