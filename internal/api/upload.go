package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/luthfi9251/ku-dump/internal/meta"
)

const maxUploadBytes = 2 << 30

var (
	pgMagic   = []byte("PGDMP")
	gzipMagic = []byte{0x1f, 0x8b}
)

func engineMatches(engine string, head []byte) bool {
	switch engine {
	case "postgres":
		return bytes.HasPrefix(head, pgMagic)
	case "mongodb":
		return bytes.HasPrefix(head, gzipMagic)
	}
	return false
}

func (s *Server) handleUploadRestore(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		fail(w, http.StatusBadRequest, "VALIDATION", "upload failed: "+err.Error())
		return
	}
	engine := r.FormValue("engine")
	if engine != "postgres" && engine != "mongodb" {
		fail(w, http.StatusBadRequest, "VALIDATION", "engine must be postgres or mongodb")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		fail(w, http.StatusBadRequest, "VALIDATION", "file field is required")
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "kudump-upload-*")
	if err != nil {
		log.Printf("create temp file: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	size, err := io.Copy(tmp, file)
	tmp.Close()
	if err != nil {
		fail(w, http.StatusBadRequest, "VALIDATION", "cannot read upload: "+err.Error())
		return
	}

	head := make([]byte, 5)
	f, err := os.Open(tmpName)
	if err != nil {
		log.Printf("open temp file: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	n, _ := f.Read(head)
	f.Close()
	if n < 2 || !engineMatches(engine, head[:n]) {
		fail(w, http.StatusBadRequest, "ENGINE_MISMATCH",
			"file content does not look like a "+engine+" dump")
		return
	}

	local, err := s.NewStore(r.Context(), 0)
	if err != nil {
		log.Printf("open local storage: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	randBuf := make([]byte, 2)
	rand.Read(randBuf)
	ext := ".dump"
	if engine == "mongodb" {
		ext = ".archive.gz"
	}
	key := fmt.Sprintf("uploads/%s-%s%s",
		time.Now().UTC().Format("20060102-150405"), hex.EncodeToString(randBuf), ext)
	if err := local.Put(r.Context(), key, tmpName); err != nil {
		log.Printf("store upload: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	label := r.FormValue("label")
	if label == "" {
		label = header.Filename
	}
	dumpID, err := s.Store.CreateDump(r.Context(), &meta.Dump{
		DatabaseID: 0, Engine: engine, Label: label, Storage: "local", Location: key,
		SourceDB: "", SizeBytes: size, Status: "uploaded", CreatedBy: userID(r),
	})
	if err != nil {
		log.Printf("create dump row: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	rows, _ := s.Store.ListDumps(r.Context())
	for _, row := range rows {
		if row.ID == dumpID {
			jsonOut(w, http.StatusCreated, dumpToDTO(row))
			return
		}
	}
	fail(w, http.StatusInternalServerError, "INTERNAL", "upload lost")
}
