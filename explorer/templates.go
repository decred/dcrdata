// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package explorer

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"html/template"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/decred/dcrd/dcrutil"

	"github.com/decred/dcrd/chaincfg"
	humanize "github.com/dustin/go-humanize"
)

type pageTemplate struct {
	file     string
	template *template.Template
}

type templates struct {
	templates map[string]pageTemplate
	defaults  []string
	folder    string
	helpers   template.FuncMap
}

func newTemplates(folder string, defaults []string, helpers template.FuncMap) templates {
	var defs []string
	for _, file := range defaults {
		defs = append(defs, filepath.Join(folder, file+".tmpl"))
	}
	return templates{
		templates: make(map[string]pageTemplate),
		defaults:  defs,
		folder:    folder,
		helpers:   helpers,
	}
}

func (t *templates) addTemplate(name string) error {
	fileName := filepath.Join(t.folder, name+".tmpl")
	files := append(t.defaults, fileName)
	temp, err := template.New(name).Funcs(t.helpers).ParseFiles(files...)
	if err == nil {
		t.templates[name] = pageTemplate{
			file:     fileName,
			template: temp,
		}
	}
	return err
}

func (t *templates) reloadTemplates() error {
	var errorStrings []string
	for fileName := range t.templates {
		err := t.addTemplate(fileName)
		if err != nil {
			errorStrings = append(errorStrings, err.Error())
		}
	}
	if errorStrings == nil {
		return nil
	}
	return fmt.Errorf(strings.Join(errorStrings, " | "))
}

// execTemplateToString executes the associated input template using the
// supplied data, and writes the result into a string. If the template fails to
// execute or isn't found, a non-nil error will be returned. Check it before
// writing to theclient, otherwise you might as well execute directly into
// your response writer instead of the internal buffer of this function.
func (t *templates) execTemplateToString(name string, data interface{}) (string, error) {
	temp, ok := t.templates[name]
	if !ok {
		return "", fmt.Errorf("Template %s not known", name)
	}

	var page bytes.Buffer
	err := temp.template.ExecuteTemplate(&page, name, data)
	return page.String(), err
}

var toInt64 = func(v interface{}) int64 {
	switch vt := v.(type) {
	case int64:
		return vt
	case int32:
		return int64(vt)
	case uint32:
		return int64(vt)
	case uint64:
		return int64(vt)
	case int:
		return int64(vt)
	case int16:
		return int64(vt)
	case uint16:
		return int64(vt)
	default:
		return math.MinInt64
	}
}

// standard golang package math in go1.9 doesn't support math.Round
func Round(val float64, places int) (newVal float64) {
	pow := math.Pow(10, float64(places))
	digit := pow * val
	newVal = math.Floor(digit) / pow
	return
}

// boldNumPlaces defines the number of decimal places to be write with same font as the whole
// number value of the float
func float64Formatting(v float64, numPlaces int, useCommas bool, boldNumPlaces ...int) []string {
	formattedVal := Round(v, numPlaces)
	clipped := fmt.Sprintf("%."+strconv.Itoa(numPlaces)+"f", formattedVal)
	oldLength := len(clipped)
	clipped = strings.TrimRight(clipped, "0")
	trailingZeros := strings.Repeat("0", oldLength-len(clipped))
	valueChunks := strings.Split(clipped, ".")
	integer := valueChunks[0]

	dec := ""
	if len(valueChunks) > 1 {
		dec = valueChunks[1]
	}

	if useCommas {
		integer = humanize.Comma(int64(formattedVal))
	}

	if len(boldNumPlaces) == 0 {
		return []string{integer, dec, trailingZeros}
	}

	places := boldNumPlaces[0]
	if places > numPlaces {
		return []string{integer, dec, trailingZeros}
	}

	if len(dec) < places {
		places = len(dec)
	}

	return []string{integer, dec[:places], dec[places:], trailingZeros}
}

func makeTemplateFuncMap(params *chaincfg.Params) template.FuncMap {
	netTheme := "theme-" + strings.ToLower(netName(params))

	return template.FuncMap{
		"add": func(a, b int64) int64 {
			return a + b
		},
		"subtract": func(a, b int64) int64 {
			return a - b
		},
		"divide": func(n, d int64) int64 {
			return n / d
		},
		"divideFloat": func(n float64, d float64) float64 {
			return n / d
		},
		"multiply": func(a, b int64) int64 {
			return a * b
		},
		"timezone": func() string {
			t, _ := time.Now().Zone()
			return t
		},
		"percentage": func(a, b int64) float64 {
			return (float64(a) / float64(b)) * 100
		},
		"int64": toInt64,
		"intComma": func(v interface{}) string {
			return humanize.Comma(toInt64(v))
		},
		"int64Comma": func(v int64) string {
			return humanize.Comma(v)
		},
		"ticketWindowProgress": func(i int) float64 {
			p := (float64(i) / float64(params.StakeDiffWindowSize)) * 100
			return p
		},
		"rewardAdjustmentProgress": func(i int) float64 {
			p := (float64(i) / float64(params.SubsidyReductionInterval)) * 100
			return p
		},
		"float64AsDecimalParts": float64Formatting,
		"amountAsDecimalParts": func(v int64, useCommas bool) []string {
			return float64Formatting(dcrutil.Amount(v).ToCoin(), 8, useCommas)
		},
		"toFloat64Amount": func(intAmount int64) float64 {
			return dcrutil.Amount(intAmount).ToCoin()
		},
		"remaining": func(idx int, max, t int64) string {
			x := (max - int64(idx)) * t
			allsecs := int(time.Duration(x).Seconds())
			str := ""
			if allsecs > 604799 {
				weeks := allsecs / 604800
				allsecs %= 604800
				str += fmt.Sprintf("%dw ", weeks)
			}
			if allsecs > 86399 {
				days := allsecs / 86400
				allsecs %= 86400
				str += fmt.Sprintf("%dd ", days)
			}
			if allsecs > 3599 {
				hours := allsecs / 3600
				allsecs %= 3600
				str += fmt.Sprintf("%dh ", hours)
			}
			if allsecs > 59 {
				mins := allsecs / 60
				allsecs %= 60
				str += fmt.Sprintf("%dm ", mins)
			}
			if allsecs > 0 {
				str += fmt.Sprintf("%ds ", allsecs)
			}
			return str + "remaining"
		},
		"TimeDurationFormat": func(duration time.Duration) (formatedDuration string) {
			durationhr := int(duration.Minutes() / 60)
			durationmin := int(duration.Minutes()) % 60
			durationsec := int(duration.Seconds()) % 60
			if durationhr != 0 {
				formatedDuration = strconv.Itoa(durationhr) + " hrs " + strconv.Itoa(durationmin) + " min " + strconv.Itoa(durationsec) + " sec"
				return
			} else if (durationhr == 0) && (durationmin != 0) {
				formatedDuration = strconv.Itoa(durationmin) + " min " + strconv.Itoa(durationsec) + " sec"
				return
			} else {
				formatedDuration = strconv.Itoa(durationsec) + " sec"
			}
			return
		},
		"covertByteArrayToString": func(arr []byte) (inString string) {
			inString = hex.EncodeToString(arr)
			return
		},
		"uint16Mul": func(a uint16, b int) (result int) {
			result = int(a) * b
			return
		},
		"TimeConversion": func(a uint64) (result time.Time) {
			result = time.Unix(int64(a), 0)
			return
		},
		"theme": func() string {
			return netTheme
		},
	}
}
