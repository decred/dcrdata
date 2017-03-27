// +build darwin dragonfly freebsd linux nacl netbsd openbsd solaris

package main

import "syscall"

// UseSIGToReloadTemplates wraps (*WebUI).UseSIGToReloadTemplates for
// non-Windows systems, where there are actually signals.
func (wu *WebUI) UseSIGToReloadTemplates() {
	wu.reloadTemplatesSig(syscall.SIGUSR1)
}
