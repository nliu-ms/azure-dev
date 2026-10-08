// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

const (
	maxMappingRecords     = 2 * maxEvaluationRows
	maxMappingCollections = 32
	maxMappingFields      = 2048
	maxMappingDepth       = 24
	maxMappingNodes       = 1_000_000
	maxMappingString      = 1024 * 1024
)

func pointerSegment(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func pointerValue(value any, pointer string) (any, bool) {
	if pointer == "" {
		return value, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	for _, segment := range strings.Split(pointer[1:], "/") {
		for i := 0; i < len(segment); i++ {
			if segment[i] == '~' {
				if i+1 == len(segment) || (segment[i+1] != '0' && segment[i+1] != '1') {
					return nil, false
				}
				i++
			}
		}
		segment = strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
		switch current := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = current[segment]
			if !ok {
				return nil, false
			}
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(current) || strconv.Itoa(index) != segment {
				return nil, false
			}
			value = current[index]
		default:
			return nil, false
		}
	}
	return value, true
}

func decodeMappingJSON(content []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("expected exactly one JSON value")
	}
	return nil
}

func profileMappingFile(index int, name string, content []byte) (mappingFile, error) {
	if len(content) == 0 || len(content) > maxEvaluationUploadBytes {
		return mappingFile{}, errors.New("evaluation file is empty or exceeds the 24 MB limit")
	}
	digest := sha256.Sum256(content)
	file := mappingFile{
		profile: MappingFileProfile{
			Index: index, Name: name, SHA256: fmt.Sprintf("%x", digest),
			Format:      strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), "."),
			Collections: []MappingCollection{},
		},
		content: content, collections: make(map[string][]map[string]any),
	}
	var err error
	switch file.profile.Format {
	case "json":
		var document any
		err = decodeMappingJSON(content, &document)
		if err == nil {
			nodes := 0
			err = validateMappingValue(document, 0, &nodes)
		}
		if err == nil {
			err = discoverMappingCollections(&file, document, "")
			file.document = document
		}
	case "jsonl":
		var rows []map[string]any
		nodes := 0
		for index, line := range bytes.Split(content, []byte{'\n'}) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var row map[string]any
			if err = decodeMappingJSON(line, &row); err != nil || row == nil {
				return file, fmt.Errorf("line %d must be a JSON object", index+1)
			}
			if err = validateMappingValue(row, 0, &nodes); err != nil {
				return file, err
			}
			rows = append(rows, row)
			if len(rows) > maxMappingRecords {
				return file, errors.New("JSONL exceeds the 20,000-record limit")
			}
		}
		file.collections[""] = rows
	case "csv":
		var rows []map[string]any
		rows, err = readMappingCSV(content)
		file.collections[""] = rows
	case "xlsx":
		file.collections, err = readMappingWorkbook(content)
	default:
		err = errors.New("unsupported evaluation container; use JSON, JSONL, CSV, or XLSX")
	}
	if err != nil {
		return file, err
	}
	total := 0
	for _, path := range slices.Sorted(maps.Keys(file.collections)) {
		rows := file.collections[path]
		total += len(rows)
		if total > maxMappingRecords {
			return file, errors.New("file exceeds the 20,000-record limit across collections")
		}
		fields, err := profileMappingFields(rows)
		if err != nil {
			return file, err
		}
		file.profile.Collections = append(file.profile.Collections, MappingCollection{
			Path: path, RowCount: len(rows), Fields: fields,
		})
	}
	if len(file.collections) == 0 {
		return file, errors.New("no record collections found; select a JSON array or a tabular file")
	}
	return file, nil
}

func validateMappingValue(value any, depth int, nodes *int) error {
	*nodes++
	if depth > maxMappingDepth || *nodes > maxMappingNodes {
		return errors.New("evaluation exceeds the nesting or value-count limit")
	}
	switch value := value.(type) {
	case string:
		if len(value) > maxMappingString {
			return errors.New("evaluation field exceeds the 1 MB string limit")
		}
	case map[string]any:
		for key, child := range value {
			if len(key) > 1024 {
				return errors.New("evaluation field name exceeds the 1024-byte limit")
			}
			if err := validateMappingValue(child, depth+1, nodes); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range value {
			if err := validateMappingValue(child, depth+1, nodes); err != nil {
				return err
			}
		}
	}
	return nil
}

func discoverMappingCollections(file *mappingFile, value any, path string) error {
	switch value := value.(type) {
	case []any:
		if len(value) > maxMappingRecords {
			return errors.New("collection exceeds the 20,000-record limit")
		}
		rows := make([]map[string]any, 0, len(value))
		for _, item := range value {
			row, ok := item.(map[string]any)
			if !ok {
				return nil
			}
			rows = append(rows, row)
		}
		file.collections[path] = rows
		if len(file.collections) > maxMappingCollections {
			return errors.New("evaluation exceeds the 32-collection limit")
		}
	case map[string]any:
		isRecord := canonicalMappingShape([]map[string]any{value}) ||
			stringAt(value, "object") == "eval.run.output_item" || isPromptfooRecord(value)
		if path == "" && isRecord {
			file.collections[""] = []map[string]any{value}
			return nil
		}
		for _, key := range slices.Sorted(maps.Keys(value)) {
			if err := discoverMappingCollections(file, value[key], path+"/"+pointerSegment(key)); err != nil {
				return err
			}
		}
		if path == "" && len(file.collections) == 0 {
			file.collections[""] = []map[string]any{value}
		}
	}
	return nil
}

func mappingValueType(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case json.Number, float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	default:
		return "object"
	}
}

func profileMappingFields(rows []map[string]any) ([]MappingField, error) {
	fields := map[string]*MappingField{}
	distinct := map[string]map[[sha256.Size]byte]struct{}{}
	var visit func(any, string) error
	visit = func(value any, path string) error {
		if path != "" {
			field := fields[path]
			if field == nil {
				if len(fields) == maxMappingFields {
					return errors.New("collection exceeds the 2048-field inventory limit")
				}
				field = &MappingField{Path: path, Types: []string{}}
				fields[path] = field
			}
			field.Present++
			kind := mappingValueType(value)
			if !slices.Contains(field.Types, kind) {
				field.Types = append(field.Types, kind)
				slices.Sort(field.Types)
			}
			if text, scalar := mappingScalar(value); scalar {
				if distinct[path] == nil {
					distinct[path] = map[[sha256.Size]byte]struct{}{}
				}
				distinct[path][sha256.Sum256([]byte(kind+"\x00"+text))] = struct{}{}
			}
			if field.Sample == "" && value != nil && kind != "array" && kind != "object" {
				field.Sample = localMappingSample(valueString(value), 160)
			}
		}
		switch value := value.(type) {
		case map[string]any:
			for _, key := range slices.Sorted(maps.Keys(value)) {
				if err := visit(value[key], path+"/"+pointerSegment(key)); err != nil {
					return err
				}
			}
		case []any:
			for i, item := range value {
				if err := visit(item, path+"/"+strconv.Itoa(i)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, row := range rows {
		if err := visit(row, ""); err != nil {
			return nil, err
		}
	}
	result := make([]MappingField, 0, len(fields))
	for _, path := range slices.Sorted(maps.Keys(fields)) {
		fields[path].Distinct = len(distinct[path])
		result = append(result, *fields[path])
	}
	return result, nil
}

func localMappingSample(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	runes := []rune(value)
	return string(runes[:min(len(runes), limit)])
}

// Repeated column labels receive ordinal-qualified names instead of overwriting values.
func mappingColumnNames(headers []string) []string {
	counts := map[string]int{}
	for _, header := range headers {
		counts[header]++
	}
	names := make([]string, len(headers))
	used := map[string]bool{}
	for index, header := range headers {
		name := header
		if name == "" || counts[header] > 1 {
			name = fmt.Sprintf("[%d] %s", index+1, header)
		}
		for used[name] || (name != header && counts[name] > 0) {
			name = "[" + strconv.Itoa(index+1) + "] " + name
		}
		used[name] = true
		names[index] = name
	}
	return names
}

func mappingTableRow(headers, cells []string) map[string]any {
	row := make(map[string]any, len(cells))
	for i, value := range cells {
		if i < len(headers) {
			row[headers[i]] = value
		}
	}
	return row
}

func readMappingCSV(content []byte) ([]map[string]any, error) {
	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf})))
	headers, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read CSV headers: %w", err)
	}
	if len(headers) > maxMappingFields {
		return nil, errors.New("CSV exceeds the 2048-column limit")
	}
	headers = mappingColumnNames(headers)
	var rows []map[string]any
	nodes := 0
	for {
		cells, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read CSV row: %w", err)
		}
		row := mappingTableRow(headers, cells)
		if err := validateMappingValue(row, 0, &nodes); err != nil {
			return nil, err
		}
		rows = append(rows, row)
		if len(rows) > maxMappingRecords {
			return nil, errors.New("CSV exceeds the 20,000-record limit")
		}
	}
	return rows, nil
}

func readMappingWorkbook(content []byte) (map[string][]map[string]any, error) {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, fmt.Errorf("open XLSX: %w", err)
	}
	files := map[string]*zip.File{}
	var expanded uint64
	for _, file := range reader.File {
		if file.UncompressedSize64 > maxWorkbookExpandedBytes-expanded {
			return nil, errors.New("workbook expands beyond the 64 MB limit")
		}
		expanded += file.UncompressedSize64
		files[file.Name] = file
	}
	var workbook xlsxWorkbook
	if err := readWorkbookXML(files, "xl/workbook.xml", &workbook); err != nil {
		return nil, err
	}
	if len(workbook.Sheets) > maxMappingCollections {
		return nil, errors.New("workbook exceeds the 32-sheet limit")
	}
	var relationships xlsxRelationships
	if err := readWorkbookXML(files, "xl/_rels/workbook.xml.rels", &relationships); err != nil {
		return nil, err
	}
	shared, err := readSharedStrings(files)
	if err != nil {
		return nil, err
	}
	result := map[string][]map[string]any{}
	nodes, total := 0, 0
	for _, sheet := range workbook.Sheets {
		target := ""
		for _, relationship := range relationships.Relationships {
			if relationship.ID == sheet.RelID {
				target = strings.TrimPrefix(relationship.Target, "/")
				if !strings.HasPrefix(target, "xl/") {
					target = "xl/" + target
				}
			}
		}
		var document xlsxWorksheet
		if err := readWorkbookXML(files, target, &document); err != nil {
			return nil, err
		}
		if len(document.Rows) == 0 {
			continue
		}
		for _, row := range document.Rows {
			for _, cell := range row.Cells {
				if len(cell.Reference) > 16 || cellColumn(cell.Reference) >= maxMappingFields {
					return nil, errors.New("workbook column exceeds the 2048-column limit")
				}
				if cell.Type == "b" && cell.Value != "0" && cell.Value != "1" {
					return nil, errors.New("workbook contains an invalid boolean cell")
				}
			}
		}
		headers := mappingColumnNames(rowValues(document.Rows[0], shared))
		rows := make([]map[string]any, 0, len(document.Rows)-1)
		for _, row := range document.Rows[1:] {
			cells := rowValues(row, shared)
			if !slices.ContainsFunc(cells, func(value string) bool { return value != "" }) {
				continue
			}
			total++
			if total > maxMappingRecords {
				return nil, errors.New("workbook exceeds the 20,000-record limit")
			}
			record := mappingTableRow(headers, cells)
			if err := validateMappingValue(record, 0, &nodes); err != nil {
				return nil, err
			}
			rows = append(rows, record)
		}
		result["/sheets/"+pointerSegment(sheet.Name)] = rows
	}
	return result, nil
}
