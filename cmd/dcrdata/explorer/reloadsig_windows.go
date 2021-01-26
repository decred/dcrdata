// +build windows

package explorer

// UseSIGToReloadTemplates wraps (*explorerUI).UseSIGToReloadTemplates for
// non-Windows systems, where there are actually signals.
func (exp *explorerUI) UseSIGToReloadTemplates() {
	log.Info("Signals unsupported on windows")
}
