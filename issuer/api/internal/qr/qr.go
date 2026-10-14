// Package qr draws the QR code that a person scans or uploads in their wallet.
package qr

import "github.com/skip2/go-qrcode"

// PNG is a square image of size pixels that holds content.
func PNG(content string, size int) ([]byte, error) {
	return qrcode.Encode(content, qrcode.Medium, size)
}
