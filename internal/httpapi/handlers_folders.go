package httpapi

import (
	"net/http"
	"strconv"
)

func folderID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// handleFolders 返回扁平文件夹列表和树形结构。
func (s *Server) handleFolders(w http.ResponseWriter, r *http.Request) {
	items, tree, err := s.deps.Domains.ListFolders(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"folders": items, "tree": tree})
}

func (s *Server) handleFolderCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		ParentID *int64 `json:"parent_id"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	folder, err := s.deps.Domains.CreateFolder(r.Context(), req.Name, req.ParentID)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusCreated, folder)
}

func (s *Server) handleFolderGet(w http.ResponseWriter, r *http.Request) {
	id, ok := folderID(r)
	if !ok {
		s.writeError(w, r, http.StatusBadRequest, "文件夹 ID 无效")
		return
	}
	items, _, err := s.deps.Domains.ListFolders(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	for _, item := range items {
		if item.ID == id {
			s.writeJSON(w, r, http.StatusOK, item)
			return
		}
	}
	s.writeError(w, r, http.StatusNotFound, "文件夹不存在")
}

func (s *Server) handleFolderUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := folderID(r)
	if !ok {
		s.writeError(w, r, http.StatusBadRequest, "文件夹 ID 无效")
		return
	}
	var req struct {
		Name     string `json:"name"`
		ParentID *int64 `json:"parent_id"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if err := s.deps.Domains.UpdateFolder(r.Context(), id, req.Name, req.ParentID); err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "success", "id": id})
}

func (s *Server) handleFolderDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := folderID(r)
	if !ok {
		s.writeError(w, r, http.StatusBadRequest, "文件夹 ID 无效")
		return
	}
	if err := s.deps.Domains.DeleteFolder(r.Context(), id); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "success", "id": id})
}

func (s *Server) handleDomainBatchMoveFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domains  []string `json:"domains"`
		FolderID *int64   `json:"folder_id"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if len(req.Domains) == 0 {
		s.writeError(w, r, http.StatusBadRequest, "没有需要移动的域名")
		return
	}
	moved, err := s.deps.Domains.MoveToFolder(r.Context(), req.Domains, req.FolderID)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"status": "success", "moved": moved, "folder_id": req.FolderID,
	})
}
