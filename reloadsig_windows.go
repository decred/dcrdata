// +build windows

package main

import "fmt"

// UseSIGToReloadTemplates wraps (*WebUI).UseSIGToReloadTemplates for Windows
// systems, where there are no signals to use.
func (wu *WebUI) UseSIGToReloadTemplates() {
	fmt.Println("Signals are unsupported on Windows.")
}

// UseSIGToReloadTemplates wraps (*explorerUI).UseSIGToReloadTemplates for Windows
// systems, where there are no signals to use.
func (exp *explorerUI) UseSIGToReloadTemplates() {
	fmt.Println("Signals are unsupported on Windows.")
}
