package qr_test

import (
	"attesta/issuer/internal/qr"
	"bytes"
	"image"
	_ "image/png"
	"testing"

	"github.com/makiuchi-d/gozxing"
	zxqrcode "github.com/makiuchi-d/gozxing/qrcode"
)

func TestTheImageHoldsTheLink(t *testing.T) {
	const link = "openid-credential-offer://?credential_offer_uri=https%3A%2F%2Fissuer.example%2Foid4vci%2Foffers%2Fabc"

	png, err := qr.PNG(link, 480)
	if err != nil {
		t.Fatal(err)
	}

	img, _, err := image.Decode(bytes.NewReader(png))
	if err != nil {
		t.Fatal(err)
	}

	if b := img.Bounds(); b.Dx() != 480 || b.Dy() != 480 {
		t.Fatalf("size %v", b)
	}

	bitmap, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		t.Fatal(err)
	}

	result, err := zxqrcode.NewQRCodeReader().Decode(bitmap, nil)
	if err != nil {
		t.Fatal(err)
	}

	if result.GetText() != link {
		t.Fatalf("decoded %q", result.GetText())
	}
}
