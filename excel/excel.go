package excel

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"bell-budut/database"
	"github.com/xuri/excelize/v2"
)

type ImportReport struct {
	TotalProcessed int      `json:"total_processed"`
	SuccessCount   int      `json:"success_count"`
	ErrorCount     int      `json:"error_count"`
	TargetGroup    string   `json:"target_group"`
	Messages       []string `json:"messages"`
}

var timeRegex = regexp.MustCompile(`^([01]?[0-9]|2[0-3]):[0-5][0-9]$`)

// GenerateTemplate builds a user-friendly Excel template with 2 sheets:
// Sheet 1: "Jadwal Bel" with dropdown pointing to "Daftar Audio" names and auto-filling Audio Code via VLOOKUP formula
// Sheet 2: "Daftar Audio" containing all current audio files dynamically from the database
func GenerateTemplate(audios []database.AudioFile, groups []database.Group, targetGroupName string) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	sheetSchedules := "Jadwal Bel"
	sheetAudios := "Daftar Audio"

	f.SetSheetName("Sheet1", sheetSchedules)
	_, err := f.NewSheet(sheetAudios)
	if err != nil {
		return nil, err
	}

	// Styles
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#FFFFFF", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#1E3A8A"}, Pattern: 1}, // Blue 900
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border: []excelize.Border{
			{Type: "left", Color: "#D1D5DB", Style: 1},
			{Type: "top", Color: "#D1D5DB", Style: 1},
			{Type: "bottom", Color: "#D1D5DB", Style: 1},
			{Type: "right", Color: "#D1D5DB", Style: 1},
		},
	})

	audioHeaderStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#FFFFFF", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#14532D"}, Pattern: 1}, // Green 900
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
	})

	autoFormulaHeaderStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#FFFFFF", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#0F766E"}, Pattern: 1}, // Teal 700 (Auto Formula)
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
	})

	// 1. Sheet "Daftar Audio"
	// Column A: Nama Audio (Used as dropdown list in Sheet 1)
	// Column B: Kode Audio (Returned by VLOOKUP)
	// Column C: Nama File
	// Column D: Keterangan
	audioHeaders := []string{"Nama Audio / Nada (PILIHAN DROPDOWN)", "Kode Audio (OTOMATIS)", "Nama File Audio", "Keterangan"}
	for i, h := range audioHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheetAudios, cell, h)
		f.SetCellStyle(sheetAudios, cell, cell, audioHeaderStyle)
	}

	audioRowsCount := len(audios)
	if audioRowsCount == 0 {
		audioRowsCount = 1
	}

	for idx, a := range audios {
		row := idx + 2
		f.SetCellValue(sheetAudios, fmt.Sprintf("A%d", row), a.Name)
		f.SetCellValue(sheetAudios, fmt.Sprintf("B%d", row), a.Code)
		f.SetCellValue(sheetAudios, fmt.Sprintf("C%d", row), a.Filename)
		f.SetCellValue(sheetAudios, fmt.Sprintf("D%d", row), a.Description)
	}

	f.SetColWidth(sheetAudios, "A", "A", 38)
	f.SetColWidth(sheetAudios, "B", "B", 18)
	f.SetColWidth(sheetAudios, "C", "C", 30)
	f.SetColWidth(sheetAudios, "D", "D", 35)

	// 2. Sheet "Jadwal Bel"
	schedHeaders := []string{
		"Grup",
		"Hari Berlaku",
		"Waktu (JJ:MM)",
		"Nama Bel / Kegiatan",
		"Pilih Nada / Lagu Audio",
		"Kode Audio (Auto)",
		"Status",
		"Keterangan",
		"Audio Lanjutan 1 (Setelah Bel Selesai)",
		"Audio Lanjutan 2 (Setelah Lanjutan 1)",
	}

	for i, h := range schedHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheetSchedules, cell, h)
		if i == 5 {
			f.SetCellStyle(sheetSchedules, cell, cell, autoFormulaHeaderStyle)
		} else {
			f.SetCellStyle(sheetSchedules, cell, cell, headerStyle)
		}
	}

	defaultGroup := "Bel Reguler"
	if targetGroupName != "" {
		defaultGroup = targetGroupName
	} else if len(groups) > 0 {
		defaultGroup = groups[0].Name
	}

	sampleAudio1 := "5 Menit Awal Kerohanian"
	sampleAudio2 := "Kerohanian Dimulai"
	sampleAudio3 := "Istirahat"
	sampleAudio4 := "Bel Pulang"
	if len(audios) > 0 {
		sampleAudio1 = audios[0].Name
		if len(audios) > 1 {
			sampleAudio2 = audios[1].Name
		}
		if len(audios) > 2 {
			sampleAudio3 = audios[2].Name
		}
	}

	sampleRows := [][]interface{}{
		{defaultGroup, "SENIN", "06:55", "Persiapan Pendidikan Karakter", sampleAudio1, "", "AKTIF", "Siswa masuk kelas"},
		{defaultGroup, "SENIN", "07:00", "Pendidikan Karakter Dimulai", sampleAudio2, "", "AKTIF", "Doa bersama"},
		{defaultGroup, "SENIN", "09:45", "Istirahat Pertama", sampleAudio3, "", "AKTIF", "Istirahat 30 menit"},
		{defaultGroup, "SENIN - JUMAT", "15:30", "Bel Pulang Sekolah", sampleAudio4, "", "AKTIF", "Selesai KBM", ""},
		{defaultGroup, "JUMAT", "11:30", "Shalat Jumat / Pulang Jumat", sampleAudio4, "", "AKTIF", "Khusus Jumat"},
	}

	for idx, r := range sampleRows {
		row := idx + 2
		for cIdx, val := range r {
			cell, _ := excelize.CoordinatesToCellName(cIdx+1, row)
			f.SetCellValue(sheetSchedules, cell, val)
		}
	}

	// Pre-fill VLOOKUP formulas for rows 2 through 150
	// Formula: =IFERROR(VLOOKUP(E2, 'Daftar Audio'!$A$2:$B$[N], 2, FALSE), "")
	for r := 2; r <= 150; r++ {
		formula := fmt.Sprintf("IFERROR(VLOOKUP(E%d, 'Daftar Audio'!$A$2:$B$%d, 2, FALSE), \"\")", r, audioRowsCount+1)
		f.SetCellFormula(sheetSchedules, fmt.Sprintf("F%d", r), formula)
	}

	f.SetColWidth(sheetSchedules, "A", "A", 20)
	f.SetColWidth(sheetSchedules, "B", "B", 18)
	f.SetColWidth(sheetSchedules, "C", "C", 16)
	f.SetColWidth(sheetSchedules, "D", "D", 32)
	f.SetColWidth(sheetSchedules, "E", "E", 38)
	f.SetColWidth(sheetSchedules, "F", "F", 20)
	f.SetColWidth(sheetSchedules, "G", "G", 14)
	f.SetColWidth(sheetSchedules, "H", "H", 28)
	f.SetColWidth(sheetSchedules, "I", "J", 38)

	// 3. Data Validations
	// Day dropdown (Col B)
	dvDays := excelize.NewDataValidation(true)
	dvDays.Sqref = "B2:B500"
	dvDays.SetDropList([]string{"SEMUA", "SENIN - JUMAT", "SENIN - KAMIS", "SELASA - JUMAT", "SENIN", "SELASA", "RABU", "KAMIS", "JUMAT", "SABTU"})
	_ = f.AddDataValidation(sheetSchedules, dvDays)

	// Audio Name dropdown (Col E) pointing to Sheet 'Daftar Audio' Column A
	if len(audios) > 0 {
		dvAudio := excelize.NewDataValidation(true)
		dvAudio.Sqref = "E2:E500"
		dvAudio.SetSqrefDropList(fmt.Sprintf("'Daftar Audio'!$A$2:$A$%d", len(audios)+1))
		_ = f.AddDataValidation(sheetSchedules, dvAudio)

		// Follow-up audio dropdown (Col I) - optional, leave empty for none
		dvNext := excelize.NewDataValidation(true)
		dvNext.Sqref = "I2:J500"
		dvNext.SetSqrefDropList(fmt.Sprintf("'Daftar Audio'!$A$2:$A$%d", len(audios)+1))
		_ = f.AddDataValidation(sheetSchedules, dvNext)
	}

	// Status dropdown (Col G)
	dvStatus := excelize.NewDataValidation(true)
	dvStatus.Sqref = "G2:G500"
	dvStatus.SetDropList([]string{"AKTIF", "NONAKTIF"})
	_ = f.AddDataValidation(sheetSchedules, dvStatus)

	f.SetActiveSheet(0)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ExportSchedules writes schedules into Excel format with human-friendly song names and reference sheet
func ExportSchedules(schedules []database.Schedule, audios []database.AudioFile, groups []database.Group, targetGroupName string) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	sheetSchedules := "Jadwal Bel"
	sheetAudios := "Daftar Audio"

	f.SetSheetName("Sheet1", sheetSchedules)
	_, _ = f.NewSheet(sheetAudios)

	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#FFFFFF", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#1E3A8A"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})

	audioHeaderStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#FFFFFF", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#14532D"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})

	autoHeaderStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#FFFFFF", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#0F766E"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})

	// Sheet Daftar Audio
	audioHeaders := []string{"Nama Audio / Nada (PILIHAN DROPDOWN)", "Kode Audio (OTOMATIS)", "Nama File Audio", "Keterangan"}
	for i, h := range audioHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheetAudios, cell, h)
		f.SetCellStyle(sheetAudios, cell, cell, audioHeaderStyle)
	}

	audioRowsCount := len(audios)
	if audioRowsCount == 0 {
		audioRowsCount = 1
	}

	for idx, a := range audios {
		row := idx + 2
		f.SetCellValue(sheetAudios, fmt.Sprintf("A%d", row), a.Name)
		f.SetCellValue(sheetAudios, fmt.Sprintf("B%d", row), a.Code)
		f.SetCellValue(sheetAudios, fmt.Sprintf("C%d", row), a.Filename)
		f.SetCellValue(sheetAudios, fmt.Sprintf("D%d", row), a.Description)
	}
	f.SetColWidth(sheetAudios, "A", "A", 38)
	f.SetColWidth(sheetAudios, "B", "B", 18)
	f.SetColWidth(sheetAudios, "C", "C", 30)
	f.SetColWidth(sheetAudios, "D", "D", 35)

	// Sheet Jadwal Bel
	schedHeaders := []string{
		"Grup",
		"Hari Berlaku",
		"Waktu (JJ:MM)",
		"Nama Bel / Kegiatan",
		"Pilih Nada / Lagu Audio",
		"Kode Audio (Auto)",
		"Status",
		"Keterangan",
		"Audio Lanjutan 1 (Setelah Bel Selesai)",
		"Audio Lanjutan 2 (Setelah Lanjutan 1)",
	}

	for i, h := range schedHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheetSchedules, cell, h)
		if i == 5 {
			f.SetCellStyle(sheetSchedules, cell, cell, autoHeaderStyle)
		} else {
			f.SetCellStyle(sheetSchedules, cell, cell, headerStyle)
		}
	}

	// Quick map of code -> audio name
	codeToName := make(map[string]string)
	for _, a := range audios {
		codeToName[strings.ToUpper(strings.TrimSpace(a.Code))] = a.Name
	}

	for idx, s := range schedules {
		row := idx + 2
		statusStr := "AKTIF"
		if !s.IsEnabled {
			statusStr = "NONAKTIF"
		}

		audioName := s.AudioName
		if audioName == "" {
			audioName = codeToName[strings.ToUpper(strings.TrimSpace(s.AudioCode))]
		}
		if audioName == "" {
			audioName = s.AudioCode
		}

		f.SetCellValue(sheetSchedules, fmt.Sprintf("A%d", row), s.GroupName)
		f.SetCellValue(sheetSchedules, fmt.Sprintf("B%d", row), s.DayOfWeek)
		f.SetCellValue(sheetSchedules, fmt.Sprintf("C%d", row), s.Time)
		f.SetCellValue(sheetSchedules, fmt.Sprintf("D%d", row), s.Title)
		f.SetCellValue(sheetSchedules, fmt.Sprintf("E%d", row), audioName)
		f.SetCellValue(sheetSchedules, fmt.Sprintf("F%d", row), s.AudioCode)
		f.SetCellValue(sheetSchedules, fmt.Sprintf("G%d", row), statusStr)
		f.SetCellValue(sheetSchedules, fmt.Sprintf("H%d", row), s.Notes)
		// Follow-up chain: one audio per column starting at I
		for ni, nc := range database.SplitAudioCodes(s.NextAudioCode) {
			nextName := codeToName[nc]
			if nextName == "" {
				nextName = nc
			}
			cell, _ := excelize.CoordinatesToCellName(9+ni, row)
			f.SetCellValue(sheetSchedules, cell, nextName)
		}
	}

	f.SetColWidth(sheetSchedules, "A", "A", 20)
	f.SetColWidth(sheetSchedules, "B", "B", 18)
	f.SetColWidth(sheetSchedules, "C", "C", 16)
	f.SetColWidth(sheetSchedules, "D", "D", 32)
	f.SetColWidth(sheetSchedules, "E", "E", 38)
	f.SetColWidth(sheetSchedules, "F", "F", 20)
	f.SetColWidth(sheetSchedules, "G", "G", 14)
	f.SetColWidth(sheetSchedules, "H", "H", 28)
	f.SetColWidth(sheetSchedules, "I", "J", 38)

	f.SetActiveSheet(0)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ImportSchedules imports schedules from Excel with support for:
// - groupMode: "specific" (assign all to targetGroupID), "new" (create newGroupName), or "file" (read from Excel column)
// - replaceAll: if true, removes existing schedules in the target group(s)
// - Resolves audio code either from Column F (Code) OR Column E (Audio Name)!
func ImportSchedules(db *database.DB, data []byte, groupMode string, targetGroupID int64, newGroupName string, replaceAll bool) (*ImportReport, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("file bukan format Excel valid: %w", err)
	}
	defer f.Close()

	sheetName := "Jadwal Bel"
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("file Excel kosong")
	}

	foundSheet := false
	for _, s := range sheets {
		if strings.EqualFold(s, sheetName) {
			sheetName = s
			foundSheet = true
			break
		}
	}
	if !foundSheet {
		sheetName = sheets[0] // Fallback to first sheet
	}

	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca sheet %s: %w", sheetName, err)
	}

	if len(rows) <= 1 {
		return nil, fmt.Errorf("tidak ada baris jadwal yang ditemukan pada file Excel")
	}

	report := &ImportReport{
		TotalProcessed: 0,
		SuccessCount:   0,
		ErrorCount:     0,
		Messages:       make([]string, 0),
	}

	// Fetch existing groups
	existingGroups, _ := db.GetGroups()
	groupMap := make(map[string]int64)
	idToGroupName := make(map[int64]string)
	for _, g := range existingGroups {
		groupMap[strings.ToLower(strings.TrimSpace(g.Name))] = g.ID
		idToGroupName[g.ID] = g.Name
	}

	// Fetch audios and build maps
	existingAudios, _ := db.GetAudioFiles()
	audioCodeMap := make(map[string]bool)
	audioNameToCode := make(map[string]string)
	for _, a := range existingAudios {
		cleanCode := strings.ToUpper(strings.TrimSpace(a.Code))
		cleanName := strings.ToLower(strings.TrimSpace(a.Name))
		audioCodeMap[cleanCode] = true
		audioNameToCode[cleanName] = a.Code
	}

	// Handle groupMode == "new"
	if groupMode == "new" {
		cleanNewName := strings.TrimSpace(newGroupName)
		if cleanNewName == "" {
			return nil, fmt.Errorf("nama grup baru tidak boleh kosong")
		}
		if existingID, exists := groupMap[strings.ToLower(cleanNewName)]; exists {
			targetGroupID = existingID
			report.TargetGroup = cleanNewName
		} else {
			newG, err := db.CreateGroup(cleanNewName, "Dibuat dari import Excel")
			if err != nil {
				return nil, fmt.Errorf("gagal membuat grup baru '%s': %w", cleanNewName, err)
			}
			targetGroupID = newG.ID
			groupMap[strings.ToLower(cleanNewName)] = newG.ID
			idToGroupName[newG.ID] = cleanNewName
			report.TargetGroup = cleanNewName
		}
	} else if groupMode == "specific" && targetGroupID > 0 {
		report.TargetGroup = idToGroupName[targetGroupID]
	}

	touchedGroups := make(map[int64]bool)

	type ValidatedRow struct {
		GroupID   int64
		Day       string
		Time      string
		Title     string
		AudioCode string
		NextCode  string
		Notes     string
		IsEnabled bool
		LineNum   int
	}

	var validRows []ValidatedRow

	// Process rows (skip header row 0)
	for idx, row := range rows[1:] {
		lineNum := idx + 2
		if len(row) == 0 {
			continue
		}

		// Detect layout:
		// 8 columns layout (New):
		// Col 0: Grup | Col 1: Hari | Col 2: Waktu | Col 3: Nama Jadwal | Col 4: Nama Audio | Col 5: Kode Audio | Col 6: Status | Col 7: Keterangan
		// 7 columns layout (Old):
		// Col 0: Grup | Col 1: Hari | Col 2: Waktu | Col 3: Nama Jadwal | Col 4: Kode Audio | Col 5: Status | Col 6: Keterangan
		var groupName, dayStr, timeStr, title, audioName, audioCode, statusStr, notes string
		var nextAudioStrs []string

		if len(row) >= 8 {
			groupName = getCol(row, 0)
			dayStr = strings.ToUpper(getCol(row, 1))
			timeStr = strings.TrimSpace(getCol(row, 2))
			title = getCol(row, 3)
			audioName = getCol(row, 4)
			audioCode = strings.ToUpper(strings.TrimSpace(getCol(row, 5)))
			statusStr = strings.ToUpper(getCol(row, 6))
			notes = getCol(row, 7)
			// optional Col 8+ : Audio Lanjutan 1, 2, ... (a cell may also hold "A, B")
			for ci := 8; ci < len(row); ci++ {
				if v := getCol(row, ci); v != "" {
					nextAudioStrs = append(nextAudioStrs, v)
				}
			}
		} else {
			groupName = getCol(row, 0)
			dayStr = strings.ToUpper(getCol(row, 1))
			timeStr = strings.TrimSpace(getCol(row, 2))
			title = getCol(row, 3)
			col4 := getCol(row, 4)
			statusStr = strings.ToUpper(getCol(row, 5))
			notes = getCol(row, 6)

			// Check if col4 is a code or a name
			if audioCodeMap[strings.ToUpper(strings.TrimSpace(col4))] {
				audioCode = strings.ToUpper(strings.TrimSpace(col4))
			} else {
				audioName = col4
			}
		}

		// Skip row if title and time are both empty
		if title == "" && timeStr == "" {
			continue
		}

		report.TotalProcessed++

		if title == "" || timeStr == "" {
			report.ErrorCount++
			report.Messages = append(report.Messages, fmt.Sprintf("Baris %d dilewati: Waktu atau Nama Bel kosong.", lineNum))
			continue
		}

		// Normalize time (e.g. 7:00 -> 07:00)
		if len(timeStr) == 4 && timeStr[1] == ':' {
			timeStr = "0" + timeStr
		}
		if !timeRegex.MatchString(timeStr) {
			report.ErrorCount++
			report.Messages = append(report.Messages, fmt.Sprintf("Baris %d dilewati: Format waktu '%s' tidak valid (gunakan format JJ:MM seperti 07:00).", lineNum, timeStr))
			continue
		}

		// Validate Day
		validDays := map[string]bool{
			"SEMUA": true, "ALL": true,
			"SENIN - JUMAT": true, "SENIN - KAMIS": true, "SELASA - JUMAT": true,
			"SENIN": true, "SELASA": true, "RABU": true, "KAMIS": true,
			"JUMAT": true, "SABTU": true, "MINGGU": true,
		}
		if dayStr == "" || !validDays[dayStr] {
			dayStr = "SEMUA"
		}
		if dayStr == "ALL" {
			dayStr = "SEMUA"
		}

		// Resolve Audio Code:
		// 1. Try code directly
		// 2. Try lookup by audio name
		// 3. Fallback to AUD-01
		resolvedCode := ""
		if audioCode != "" && audioCodeMap[audioCode] {
			resolvedCode = audioCode
		} else if audioName != "" {
			if c, ok := audioNameToCode[strings.ToLower(strings.TrimSpace(audioName))]; ok {
				resolvedCode = c
			}
		}

		if resolvedCode == "" {
			if audioCode != "" {
				if c, ok := audioNameToCode[strings.ToLower(strings.TrimSpace(audioCode))]; ok {
					resolvedCode = c
				}
			}
		}

		if resolvedCode == "" {
			resolvedCode = "AUD-01"
			report.Messages = append(report.Messages, fmt.Sprintf("Baris %d peringatan: Nada '%s' / '%s' tidak ditemukan di database, dialihkan ke AUD-01.", lineNum, audioName, audioCode))
		}

		// Resolve optional follow-up audio (by name or code)
		var nextCodes []string
		for _, nv := range nextAudioStrs {
			if c, ok := audioNameToCode[strings.ToLower(nv)]; ok { // whole cell is one audio name
				nextCodes = append(nextCodes, c)
				continue
			}
			for _, part := range strings.FieldsFunc(nv, func(r rune) bool { return r == ',' || r == ';' || r == '|' }) {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				if audioCodeMap[strings.ToUpper(part)] {
					nextCodes = append(nextCodes, strings.ToUpper(part))
				} else if c, ok := audioNameToCode[strings.ToLower(part)]; ok {
					nextCodes = append(nextCodes, c)
				} else {
					report.Messages = append(report.Messages, fmt.Sprintf("Baris %d peringatan: Audio lanjutan '%s' tidak ditemukan, diabaikan.", lineNum, part))
				}
			}
		}
		nextCode := database.JoinAudioCodes(nextCodes)

		// Resolve Group ID:
		var gID int64
		if (groupMode == "specific" || groupMode == "new") && targetGroupID > 0 {
			gID = targetGroupID
		} else {
			// groupMode == "file"
			if groupName == "" {
				groupName = "Bel Reguler"
			}
			gKey := strings.ToLower(strings.TrimSpace(groupName))
			existingID, exists := groupMap[gKey]
			if !exists {
				newG, err := db.CreateGroup(groupName, "Dibuat otomatis dari import Excel")
				if err != nil {
					report.ErrorCount++
					report.Messages = append(report.Messages, fmt.Sprintf("Baris %d error: Gagal membuat grup '%s': %v", lineNum, groupName, err))
					continue
				}
				gID = newG.ID
				groupMap[gKey] = gID
				idToGroupName[gID] = groupName
			} else {
				gID = existingID
			}
		}

		touchedGroups[gID] = true

		isEnabled := true
		if statusStr == "NONAKTIF" || statusStr == "DISABLED" || statusStr == "FALSE" {
			isEnabled = false
		}

		validRows = append(validRows, ValidatedRow{
			GroupID:   gID,
			Day:       dayStr,
			Time:      timeStr,
			Title:     title,
			AudioCode: resolvedCode,
			NextCode:  nextCode,
			Notes:     notes,
			IsEnabled: isEnabled,
			LineNum:   lineNum,
		})
	}

	// Execute DB insert
	if replaceAll {
		for gID := range touchedGroups {
			schedules, _ := db.GetSchedules(gID, "")
			for _, s := range schedules {
				_ = db.DeleteSchedule(s.ID)
			}
		}
	}

	for _, vr := range validRows {
		_, err := db.CreateSchedule(&database.Schedule{
			GroupID:       vr.GroupID,
			DayOfWeek:     vr.Day,
			Time:          vr.Time,
			Title:         vr.Title,
			AudioCode:     vr.AudioCode,
			NextAudioCode: vr.NextCode,
			Notes:         vr.Notes,
			IsEnabled:     vr.IsEnabled,
		})
		if err != nil {
			report.ErrorCount++
			report.Messages = append(report.Messages, fmt.Sprintf("Baris %d gagal disimpan: %v", vr.LineNum, err))
		} else {
			report.SuccessCount++
		}
	}

	targetName := report.TargetGroup
	if targetName == "" && len(touchedGroups) > 0 {
		var names []string
		for gID := range touchedGroups {
			names = append(names, idToGroupName[gID])
		}
		targetName = strings.Join(names, ", ")
	}

	report.Messages = append(report.Messages, fmt.Sprintf("Selesai: Berhasil memasukkan %d jadwal ke grup [%s] (%d gagal/dilewati).", report.SuccessCount, targetName, report.ErrorCount))
	return report, nil
}

func getCol(row []string, idx int) string {
	if idx < len(row) {
		return strings.TrimSpace(row[idx])
	}
	return ""
}
