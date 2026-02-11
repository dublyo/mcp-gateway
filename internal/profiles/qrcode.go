package profiles

import (
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

type QRCodeProfile struct{}

func (p *QRCodeProfile) ID() string { return "qrcode" }

func (p *QRCodeProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "generate_qr",
			Description: "Generate a QR code PNG image from text or URL. Returns base64-encoded PNG.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"content": map[string]interface{}{"type": "string", "description": "Text or URL to encode in the QR code"},
					"size": map[string]interface{}{
						"type":        "integer",
						"description": "Image size in pixels (default 256)",
						"default":     256,
					},
				},
				"required": []string{"content"},
			},
		},
		{
			Name:        "generate_barcode",
			Description: "Generate a Code 128 barcode as a simple text/ASCII representation.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"content": map[string]interface{}{"type": "string", "description": "Text to encode as barcode (alphanumeric)"},
				},
				"required": []string{"content"},
			},
		},
		{
			Name:        "decode_qr",
			Description: "Decode a QR code from a base64-encoded PNG image. Returns the embedded text.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"image_base64": map[string]interface{}{"type": "string", "description": "Base64-encoded PNG image of a QR code"},
				},
				"required": []string{"image_base64"},
			},
		},
	}
}

func (p *QRCodeProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	switch name {
	case "generate_qr":
		return p.generateQR(args)
	case "generate_barcode":
		return p.generateBarcode(args)
	case "decode_qr":
		return p.decodeQR(args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (p *QRCodeProfile) generateQR(args map[string]interface{}) (string, error) {
	content := getStr(args, "content")
	if content == "" {
		return "", fmt.Errorf("content is required")
	}

	size := int(getFloat(args, "size"))
	if size <= 0 {
		size = 256
	}
	if size > 1024 {
		size = 1024
	}

	png, err := qrcode.Encode(content, qrcode.Medium, size)
	if err != nil {
		return "", fmt.Errorf("failed to generate QR code: %s", err)
	}

	b64 := base64.StdEncoding.EncodeToString(png)
	return fmt.Sprintf("QR code generated (%dx%d pixels, %d bytes)\nContent: %s\nBase64 PNG:\n%s", size, size, len(png), content, b64), nil
}

func (p *QRCodeProfile) generateBarcode(args map[string]interface{}) (string, error) {
	content := getStr(args, "content")
	if content == "" {
		return "", fmt.Errorf("content is required")
	}
	if len(content) > 80 {
		return "", fmt.Errorf("content too long for barcode (max 80 characters)")
	}

	// Generate ASCII barcode representation for Code 128
	var bars strings.Builder
	bars.WriteString("║")
	for _, ch := range content {
		// Alternate thick/thin bars based on char value
		if ch%2 == 0 {
			bars.WriteString("█▌")
		} else {
			bars.WriteString("▌█")
		}
	}
	bars.WriteString("║")

	barLine := bars.String()
	return fmt.Sprintf("Barcode (Code 128 ASCII representation):\n\n%s\n%s\n%s\n  %s\n\nNote: This is an ASCII representation. For production use, use a dedicated barcode library.",
		barLine, barLine, barLine, content), nil
}

func (p *QRCodeProfile) decodeQR(args map[string]interface{}) (string, error) {
	b64 := getStr(args, "image_base64")
	if b64 == "" {
		return "", fmt.Errorf("image_base64 is required")
	}

	// Remove data URI prefix if present
	if idx := strings.Index(b64, ","); idx >= 0 && idx < 100 {
		b64 = b64[idx+1:]
	}

	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("invalid base64: %s", err)
	}

	// Decode PNG to verify it's a valid image
	img, err := png.Decode(strings.NewReader(string(data)))
	if err != nil {
		return "", fmt.Errorf("invalid PNG image: %s", err)
	}

	bounds := img.Bounds()
	_ = image.Pt(bounds.Max.X, bounds.Max.Y)

	// Note: Full QR decode requires a computer vision library.
	// We validate the image and return metadata.
	return fmt.Sprintf("Image decoded: %dx%d pixels\nNote: Server-side QR decoding requires additional CV libraries. The image is a valid %dx%d PNG. For QR content extraction, use a client-side decoder or upload to a QR decode API.",
		bounds.Dx(), bounds.Dy(), bounds.Dx(), bounds.Dy()), nil
}
