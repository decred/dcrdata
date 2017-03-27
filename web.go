package main

import (
	"bytes"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/dcrdata/dcrdata/blockdata"
	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
)

func TemplateExecToString(t *template.Template, name string, data interface{}) (string, error) {
	var page bytes.Buffer
	err := t.ExecuteTemplate(&page, name, data)
	return page.String(), err
}

type WebTemplateData struct {
	BlockSummary   apitypes.BlockDataBasic
	StakeSummary   apitypes.StakeInfoExtendedEstimates
	MempoolFeeInfo apitypes.MempoolTicketFeeInfo
	MempoolFees    apitypes.MempoolTicketFees
}

type WebUI struct {
	TemplateData WebTemplateData
	templ        *template.Template
	templFiles   []string
}

func NewWebUI() *WebUI {
	fp := filepath.Join("views", "root.tmpl")
	tmpl, err := template.New("home").ParseFiles(fp)
	if err != nil {
		return nil
	}

	//var templFiles []string
	templFiles := []string{fp}

	return &WebUI{
		templ:      tmpl,
		templFiles: templFiles,
	}
}

func (td *WebUI) ParseTemplates() (err error) {
	td.templ, err = template.New("home").ParseFiles(td.templFiles...)
	return
}

// See reloadsig*.go for an exported method
func (td *WebUI) reloadTemplatesSig(sig os.Signal) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, sig)

	go func() {
		for {
			sigr := <-sigChan
			log.Infof("Received %s", sig)
			if sigr == sig {
				if err := td.ParseTemplates(); err != nil {
					log.Error(err)
					continue
				}
				log.Infof("Web UI html templates reparsed.")
			}
		}
	}()
}

func (td *WebUI) Store(blockData *blockdata.BlockData) error {
	td.TemplateData.BlockSummary = blockData.ToBlockSummary()
	td.TemplateData.StakeSummary = blockData.ToStakeInfoExtendedEstimates()
	return nil
}

func (td *WebUI) RootPage(w http.ResponseWriter, r *http.Request) {
	//err := td.templ.Execute(w, td.TemplateData)
	str, err := TemplateExecToString(td.templ, "home", td.TemplateData)
	if err != nil {
		http.Error(w, "template execute failure", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, str)
}
