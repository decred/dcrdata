// +build darwin dragonfly freebsd linux nacl netbsd openbsd solaris

package explorer

import "syscall"

// UseSIGToReloadTemplates wraps (*explorerUI).UseSIGToReloadTemplates for
// non-Windows systems, where there are actually signals.
func (exp *explorerUI) UseSIGToReloadTemplates() {
	exp.reloadTemplatesSig(syscall.SIGUSR1)
}
