package api

import (
	"crypto/rand"
	"encoding/hex"
	"image"
	_ "image/gif"  // register GIF decoder for image.Decode
	_ "image/jpeg" // register JPEG decoder for image.Decode
	_ "image/png"  // register PNG decoder for image.Decode
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/HugoSmits86/nativewebp"
	_ "golang.org/x/image/webp" // register WebP decoder for image.Decode
)

const maxUploadBytes = 10 << 20         // 10 MiB for image uploads
const maxDownloadFileBytes = 100 << 20  // 100 MiB for digital downloads

// allowedDownloadExt lists file types we accept on /api/admin/upload-file.
// Images go through the existing /api/admin/upload endpoint (which
// re-encodes to WebP); this endpoint stores the bytes verbatim and is
// meant for digital-download payloads, library files, and similar.
var allowedDownloadExt = map[string]bool{
	".pdf":  true,
	".zip":  true,
	".epub": true,
	".mobi": true,
	".docx": true,
	".xlsx": true,
	".pptx": true,
	".txt":  true,
	".csv":  true,
	".mp3":  true,
	".mp4":  true,
	".m4a":  true,
	".wav":  true,
}

// allowedImageExt lists the input formats we accept; every successful
// upload is re-encoded to WebP regardless of what came in. The .webp
// entry is intentional — re-encoding a WebP normalises it.
var allowedImageExt = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".webp": true,
}

// uploadImage accepts a multipart "file" field, decodes the image (JPG /
// PNG / GIF / WebP), re-encodes it as WebP, and saves only the .webp
// version to disk. The original bytes are never persisted.
func (a *API) uploadImage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or invalid form (max 10 MB)")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing 'file' field")
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedImageExt[ext] {
		writeError(w, http.StatusBadRequest, "unsupported file type (use jpg, png, gif, or webp)")
		return
	}

	// Decode the upload. image.Decode dispatches by the magic bytes in
	// the header, not the extension — so a mis-named .png that's really
	// a JPEG still works, and a corrupted file is rejected here.
	img, _, err := image.Decode(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not decode image: "+err.Error())
		return
	}

	// Generate a unique filename — always .webp regardless of input.
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate file name")
		return
	}
	name := time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(buf) + ".webp"
	path := filepath.Join(a.cfg.UploadDir, name)

	dst, err := os.Create(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save file")
		return
	}

	// nativewebp produces lossless VP8L WebP. BestSpeed keeps admin
	// uploads snappy (DefaultCompression was ~30s on a small VPS for a
	// modest image); the file-size penalty is acceptable for display-sized
	// uploads. Bump to DefaultCompression or BestCompression if size
	// matters more than upload latency.
	if err := nativewebp.Encode(dst, img, &nativewebp.Options{
		CompressionLevel: nativewebp.BestSpeed,
	}); err != nil {
		// Close + clean up so a half-written file doesn't linger on disk.
		_ = dst.Close()
		_ = os.Remove(path)
		writeError(w, http.StatusInternalServerError, "could not encode webp: "+err.Error())
		return
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(path)
		writeError(w, http.StatusInternalServerError, "could not finalise file: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"url": "/uploads/" + name})
}

// uploadFile accepts arbitrary file types for digital downloads,
// library files, and similar non-image payloads. The bytes are stored
// verbatim — unlike uploadImage we don't decode or re-encode. Returns
// the public URL, the original filename, and the size in bytes.
func (a *API) uploadFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxDownloadFileBytes)
	if err := r.ParseMultipartForm(maxDownloadFileBytes); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or invalid form (max 100 MB)")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing 'file' field")
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedDownloadExt[ext] {
		writeError(w, http.StatusBadRequest, "file type not allowed")
		return
	}

	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate file name")
		return
	}
	name := time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(buf) + ext
	path := filepath.Join(a.cfg.UploadDir, name)

	dst, err := os.Create(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save file")
		return
	}
	n, err := io.Copy(dst, file)
	if err != nil {
		_ = dst.Close()
		_ = os.Remove(path)
		writeError(w, http.StatusInternalServerError, "could not write file")
		return
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(path)
		writeError(w, http.StatusInternalServerError, "could not finalise file: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"url":       "/uploads/" + name,
		"filename":  header.Filename,
		"sizeBytes": n,
	})
}
