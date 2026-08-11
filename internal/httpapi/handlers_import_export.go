package httpapi

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"DomainHunter/internal/service"
)

func (s *Server) handleDomainExport(w http.ResponseWriter, r *http.Request) {
	records, err := s.deps.Domains.ExportDomains(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "json"
	}
	switch format {
	case "json":
		writeDownloadJSON(w, r, "domainhunter-domains.json", records)
	case "csv":
		s.writeDomainCSV(w, r, records)
	default:
		s.writeError(w, r, http.StatusBadRequest, "format 仅支持 csv 或 json")
	}
}

func writeDownloadJSON(w http.ResponseWriter, r *http.Request, filename string, value any) {
	safeFilename := strings.ReplaceAll(filename, "\"", "")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeFilename+`"`)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) writeDomainCSV(w http.ResponseWriter, r *http.Request, records []service.DomainExportRecord) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{"domain", "status", "registrar", "folder_id", "folder", "tags", "note", "enabled", "notify", "added_at", "last_checked"})
	for _, item := range records {
		folderID := ""
		if item.FolderID != nil {
			folderID = strconv.FormatInt(*item.FolderID, 10)
		}
		_ = writer.Write([]string{
			item.Domain, string(item.Status), item.Registrar, folderID, item.Folder,
			strings.Join(item.Tags, ";"), item.Note, strconv.FormatBool(item.Enabled),
			strconv.FormatBool(item.Notify), formatTime(item.AddedAt), formatTime(item.LastChecked),
		})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="domainhunter-domains.csv"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buffer.Bytes())
}

func (s *Server) handleDomainImport(w http.ResponseWriter, r *http.Request) {
	records, mode, err := decodeDomainImport(r)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if len(records) == 0 {
		s.writeError(w, r, http.StatusBadRequest, "导入文件中没有域名")
		return
	}
	result, err := s.deps.Domains.ImportDomains(r.Context(), records, mode)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "success", "result": result})
}

func decodeDomainImport(r *http.Request) ([]service.DomainImportRecord, string, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20+1))
	if err != nil {
		return nil, "", fmt.Errorf("读取导入文件失败: %w", err)
	}
	defer r.Body.Close()
	if len(body) > 16<<20 {
		return nil, "", fmt.Errorf("导入文件不能超过16MB")
	}
	mode := strings.TrimSpace(r.URL.Query().Get("mode"))
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	if format == "" && strings.Contains(contentType, "json") {
		format = "json"
	}
	trimmed := bytes.TrimSpace(body)
	if format == "json" || (format == "" && len(trimmed) > 0 && (trimmed[0] == '[' || trimmed[0] == '{')) {
		var direct []service.DomainImportRecord
		if err := json.Unmarshal(trimmed, &direct); err == nil {
			return direct, mode, nil
		}
		var payload struct {
			Mode    string                       `json:"mode"`
			Domains []service.DomainImportRecord `json:"domains"`
			Records []service.DomainImportRecord `json:"records"`
		}
		if err := json.Unmarshal(trimmed, &payload); err != nil {
			return nil, "", fmt.Errorf("JSON 导入格式无效: %w", err)
		}
		if mode == "" {
			mode = payload.Mode
		}
		if len(payload.Domains) > 0 {
			return payload.Domains, mode, nil
		}
		return payload.Records, mode, nil
	}
	reader := csv.NewReader(bytes.NewReader(body))
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, "", fmt.Errorf("CSV 导入格式无效: %w", err)
	}
	if len(rows) < 1 {
		return nil, mode, nil
	}
	header := make(map[string]int, len(rows[0]))
	for i, item := range rows[0] {
		header[strings.ToLower(strings.TrimSpace(item))] = i
	}
	if _, ok := header["domain"]; !ok {
		return nil, "", fmt.Errorf("CSV 必须包含 domain 列")
	}
	var records []service.DomainImportRecord
	for _, row := range rows[1:] {
		item := service.DomainImportRecord{Domain: csvValue(row, header, "domain")}
		item.Folder = csvValue(row, header, "folder")
		if value := csvValue(row, header, "folder_id"); value != "" {
			id, parseErr := strconv.ParseInt(value, 10, 64)
			if parseErr != nil || id <= 0 {
				return nil, "", fmt.Errorf("CSV folder_id 无效: %s", value)
			}
			item.FolderID = &id
		}
		if value, ok := csvValueOK(row, header, "tags"); ok {
			item.Tags = splitImportTags(value)
		}
		if value, ok := csvValueOK(row, header, "note"); ok {
			item.Note = &value
		}
		if value, ok := csvValueOK(row, header, "enabled"); ok && value != "" {
			parsed, parseErr := strconv.ParseBool(value)
			if parseErr != nil {
				return nil, "", fmt.Errorf("CSV enabled 无效: %s", value)
			}
			item.Enabled = &parsed
		}
		if value, ok := csvValueOK(row, header, "notify"); ok && value != "" {
			parsed, parseErr := strconv.ParseBool(value)
			if parseErr != nil {
				return nil, "", fmt.Errorf("CSV notify 无效: %s", value)
			}
			item.Notify = &parsed
		}
		records = append(records, item)
	}
	return records, mode, nil
}

func csvValue(row []string, header map[string]int, key string) string {
	value, _ := csvValueOK(row, header, key)
	return value
}

func csvValueOK(row []string, header map[string]int, key string) (string, bool) {
	index, ok := header[key]
	if !ok || index < 0 || index >= len(row) {
		return "", false
	}
	return strings.TrimSpace(row[index]), true
}

func splitImportTags(raw string) []string {
	separator := ";"
	if !strings.Contains(raw, ";") {
		separator = ","
	}
	var out []string
	for _, item := range strings.Split(raw, separator) {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func formatTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02T15:04:05Z07:00")
}

func formatTimeValue(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02T15:04:05Z07:00")
}

func (s *Server) handleDomainHistoryExport(w http.ResponseWriter, r *http.Request) {
	limit := 500
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 5000 {
			s.writeError(w, r, http.StatusBadRequest, "limit 无效")
			return
		}
		limit = parsed
	}
	history, err := s.deps.Domains.History(r.Context(), r.PathValue("domain"), limit)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" || format == "json" {
		writeDownloadJSON(w, r, "domainhunter-history.json", history)
		return
	}
	if format != "csv" {
		s.writeError(w, r, http.StatusBadRequest, "format 仅支持 csv 或 json")
		return
	}
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{"id", "domain", "status", "registrar", "provider", "confidence", "changed", "observed_at", "registered_at", "updated_at", "expiry_at", "name_servers"})
	for _, item := range history {
		_ = writer.Write([]string{
			strconv.FormatInt(item.ID, 10), item.Domain, string(item.Status), item.Registrar,
			item.Provider, string(item.Confidence), strconv.FormatBool(item.Changed),
			formatTimeValue(item.ObservedAt), formatTime(item.RegisteredAt), formatTime(item.UpdatedAt),
			formatTime(item.ExpiryAt), strings.Join(item.NameServers, ";"),
		})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="domainhunter-history.csv"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buffer.Bytes())
}
