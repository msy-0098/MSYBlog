package handler

import (
	"bytes"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	_ "golang.org/x/image/webp"

	"masenyu.top/blog/backend/internal/model"
	"masenyu.top/blog/backend/internal/response"
)

type UploadDTO struct {
	ID       uint   `json:"id"`
	Filename string `json:"filename"`
	Path     string `json:"path"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
}

func (h AdminContentHandler) UploadImage(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		badRequest(c)
		return
	}
	defer file.Close()

	mimeType := header.Header.Get("Content-Type")
	if !strings.HasPrefix(mimeType, "image/") {
		response.Error(c, http.StatusBadRequest, 400, "只允许上传图片")
		return
	}

	content, err := io.ReadAll(io.LimitReader(file, 5*1024*1024+1))
	if err != nil {
		internalError(c)
		return
	}
	if len(content) > 5*1024*1024 {
		response.Error(c, http.StatusBadRequest, 400, "图片不能超过 5MB")
		return
	}

	if _, _, ok := detectSafeImage(content); !ok {
		response.Error(c, http.StatusBadRequest, 400, "only image uploads are allowed")
		return
	}

	// Re-encode to strip EXIF/metadata and reject non-decodable payloads.
	safeBytes, mimeType, ext, err := reencodeSafeImage(content)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 400, "图片无法解析或重编码失败")
		return
	}

	if err := os.MkdirAll("uploads", 0o755); err != nil {
		internalError(c)
		return
	}

	filename := strconv.FormatInt(time.Now().UnixNano(), 10) + ext
	diskPath := filepath.Join("uploads", filename)
	if err := os.WriteFile(diskPath, safeBytes, 0o644); err != nil {
		internalError(c)
		return
	}

	upload := model.Upload{
		Filename: header.Filename,
		Path:     "/uploads/" + filename,
		MimeType: mimeType,
		Size:     int64(len(safeBytes)),
	}
	if err := h.db.Create(&upload).Error; err != nil {
		internalError(c)
		return
	}

	response.Success(c, UploadDTO{
		ID:       upload.ID,
		Filename: upload.Filename,
		Path:     upload.Path,
		MimeType: upload.MimeType,
		Size:     upload.Size,
	})
}

func detectSafeImage(content []byte) (string, string, bool) {
	mimeType := http.DetectContentType(content)
	switch mimeType {
	case "image/jpeg":
		return mimeType, ".jpg", true
	case "image/png":
		return mimeType, ".png", true
	case "image/gif":
		return mimeType, ".gif", true
	case "image/webp":
		return mimeType, ".webp", true
	default:
		return "", "", false
	}
}

// reencodeSafeImage decodes then re-encodes the image so EXIF and exotic
// containers never reach disk. JPEG stays JPEG; everything else becomes PNG.
func reencodeSafeImage(content []byte) ([]byte, string, string, error) {
	img, format, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return nil, "", "", err
	}

	var buf bytes.Buffer
	switch strings.ToLower(format) {
	case "jpeg", "jpg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 88}); err != nil {
			return nil, "", "", err
		}
		return buf.Bytes(), "image/jpeg", ".jpg", nil
	default:
		if err := png.Encode(&buf, img); err != nil {
			return nil, "", "", err
		}
		return buf.Bytes(), "image/png", ".png", nil
	}
}
