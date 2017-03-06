package main

import (
	"bytes"
	"html/template"
	"io"
	"net/http"
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
	BlockSummary apitypes.BlockDataBasic
	StakeSummary apitypes.StakeInfoExtendedEstimates
}

type WebUI struct {
	TemplateData WebTemplateData
	templ        *template.Template
}

func NewWebUI() *WebUI {
	fp := filepath.Join("views", "root.tmpl")
	tmpl, err := template.New("home").ParseFiles(fp)
	if err != nil {
		return nil
	}

	return &WebUI{
		templ: tmpl,
	}
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
