package main

import (
	"path/filepath"
	"html/template"
	"net/http"

	"github.com/dcrdata/dcrdata/blockdata"
	apitypes "github.com/dcrdata/dcrdata/dcrdataapi"
)

type TemplateData struct {
	blockSummary apitypes.BlockDataBasic
	stakeSummary apitypes.StakeInfoExtendedEstimates
}

type WebUI struct {
	templateData TemplateData
	templ        *template.Template
}

func NewWebUI() *WebUI {
	fp := filepath.Join("views", "root.html")
	tmpl, err := template.New("home").ParseFiles(fp)
	if err != nil {
		return nil
	}

	return &WebUI{
		templ: tmpl,
	}
}

func (td *WebUI) Store(blockData *blockdata.BlockData) error {
	td.templateData.blockSummary = blockData.ToBlockSummary()
	td.templateData.stakeSummary = blockData.ToStakeInfoExtendedEstimates()
	return nil
}

func (td *WebUI) RootPage(w http.ResponseWriter, r *http.Request) {
	err := td.templ.Execute(w, td.templateData)
	if err != nil {
		http.Error(w, "execute failure", http.StatusInternalServerError)
	}
}
