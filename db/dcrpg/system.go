package dcrpg

import (
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/decred/dcrdata/v4/db/dcrpg/internal"
)

func parseUnit(unit string) (multiple float64, baseUnit string, err error) {
	re := regexp.MustCompile(`([-\d\.]*)\s*(.*)`)
	matches := re.FindStringSubmatch(unit)

	if len(matches) != 3 {
		panic("inconceivable!")
	}

	baseUnit = strings.TrimSuffix(matches[2], " ")

	switch matches[1] {
	case "":
		multiple = 1
	case "-":
		multiple = -1
	default:
		multiple, err = strconv.ParseFloat(matches[1], 64)
		if err != nil {
			baseUnit = ""
		}
	}

	return
}

type PGSetting struct {
	Name, Setting, Unit, ShortDesc, Source, SourceFile, SourceLine string
}

type PGSettings map[string]PGSetting

func (pgs PGSettings) String() string {
	numSettings := len(pgs)
	names := make([]string, 0, numSettings)
	for name := range pgs {
		names = append(names, name)
	}

	sort.Strings(names)

	// Determine max width of "Setting", "Name", and "File" entries.
	fileWidth, nameWidth, settingWidth := 4, 4, 7
	fullSettings := make([]string, 0, numSettings)
	for i := range names {
		s, ok := pgs[names[i]]
		if !ok {
			log.Warnf("(PGSettings).String is not thread-safe!")
			continue
		}

		// See if setting is numeric.
		fullSetting := s.Setting
		num1, err := strconv.ParseFloat(s.Setting, 64)
		if err == nil {
			// Combine with the unit if numeric.
			num2, unit, err := parseUnit(s.Unit)
			if err == nil {
				if unit != "" {
					unit = " " + unit
				}
				fullSetting = fmt.Sprintf("%.12g%s", num1*num2, unit)
			}
		}

		fullSettings = append(fullSettings, fullSetting)

		if len(fullSetting) > settingWidth {
			settingWidth = len(fullSetting)
		}

		if len(s.SourceFile) > fileWidth {
			fileWidth = len(s.SourceFile)
		}
		if len(s.Name) > nameWidth {
			nameWidth = len(s.Name)
		}
	}

	format := "%" + strconv.Itoa(nameWidth) + "s | %" + strconv.Itoa(settingWidth) +
		"s | %10.10s | %" + strconv.Itoa(fileWidth) + "s | %5s | %-48.48s\n"

	// Write the headers and a horizontal bar.
	out := fmt.Sprintf(format, "Name", "Setting", "Source", "File", "Line", "Description")
	hbar := strings.Repeat(string([]rune{0x2550}), nameWidth+1) + string([]rune{0x256A}) +
		strings.Repeat(string([]rune{0x2550}), settingWidth+2) + string([]rune{0x256A}) +
		strings.Repeat(string([]rune{0x2550}), 12) + string([]rune{0x256A}) +
		strings.Repeat(string([]rune{0x2550}), fileWidth+2) + string([]rune{0x256A}) +
		strings.Repeat(string([]rune{0x2550}), 7) + string([]rune{0x256A}) +
		strings.Repeat(string([]rune{0x2550}), 50)
	out += hbar + "\n"

	// Write each row.
	for i := range names {
		s, ok := pgs[names[i]]
		if !ok {
			log.Warnf("(PGSettings).String is not thread-safe!")
			continue
		}
		out += fmt.Sprintf(format, s.Name, fullSettings[i], s.Source,
			s.SourceFile, s.SourceLine, s.ShortDesc)
	}
	return out
}

func RetrievePGVersion(db *sql.DB) (ver string, err error) {
	err = db.QueryRow(internal.RetrievePGVersion).Scan(&ver)
	return
}

func retrieveSysSettings(stmt string, db *sql.DB) (PGSettings, error) {
	rows, err := db.Query(stmt)
	if err != nil {
		return nil, err
	}

	defer closeRows(rows)

	settings := make(PGSettings)

	for rows.Next() {
		var name, setting, unit, short_desc, source, sourcefile sql.NullString
		var sourceline sql.NullInt64
		err = rows.Scan(&name, &setting, &unit, &short_desc,
			&source, &sourcefile, &sourceline)
		if err != nil {
			return nil, err
		}

		var line, file string
		if source.String == "configuration file" {
			source.String = "conf file"
			if sourcefile.String == "" {
				file = "NO PERMISION"
			} else {
				file = sourcefile.String
				line = strconv.FormatInt(sourceline.Int64, 10)
			}
		}

		settings[name.String] = PGSetting{
			Name:       name.String,
			Setting:    setting.String,
			Unit:       unit.String,
			ShortDesc:  short_desc.String,
			Source:     source.String,
			SourceFile: file,
			SourceLine: line,
		}
	}

	return settings, nil
}

func RetrieveSysSettingsConfFile(db *sql.DB) (PGSettings, error) {
	return retrieveSysSettings(internal.RetrieveSysSettingsConfFile, db)
}

func RetrieveSysSettingsPerformance(db *sql.DB) (PGSettings, error) {
	return retrieveSysSettings(internal.RetrieveSysSettingsPerformance, db)
}

func RetrieveSysSettingsServer(db *sql.DB) (PGSettings, error) {
	return retrieveSysSettings(internal.RetrieveSysSettingsServer, db)
}
