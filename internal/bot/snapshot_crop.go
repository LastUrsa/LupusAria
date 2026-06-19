package bot

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"math"
)

func cropSnapshot(data []byte, crop SnapshotCrop) ([]byte, string, error) {
	if len(data) == 0 {
		return nil, "", fmt.Errorf("snapshot image is empty")
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("decode snapshot: %w", err)
	}
	bounds := src.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil, "", fmt.Errorf("snapshot has invalid dimensions")
	}

	rect, err := cropRect(width, height, crop)
	if err != nil {
		return nil, "", err
	}
	dst := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(dst, dst.Bounds(), src, rect.Min, draw.Src)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 90}); err != nil {
		return nil, "", fmt.Errorf("encode cropped snapshot: %w", err)
	}
	return out.Bytes(), "image/jpeg", nil
}

func cropRect(imageWidth, imageHeight int, crop SnapshotCrop) (image.Rectangle, error) {
	if crop.Width <= 0 || crop.Height <= 0 {
		return image.Rectangle{}, fmt.Errorf("snapshot crop width and height must be greater than zero")
	}
	x := int(math.Round(crop.X * float64(imageWidth)))
	y := int(math.Round(crop.Y * float64(imageHeight)))
	w := int(math.Round(crop.Width * float64(imageWidth)))
	h := int(math.Round(crop.Height * float64(imageHeight)))

	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if x >= imageWidth {
		x = imageWidth - 1
	}
	if y >= imageHeight {
		y = imageHeight - 1
	}
	maxX := x + w
	maxY := y + h
	if maxX > imageWidth {
		maxX = imageWidth
	}
	if maxY > imageHeight {
		maxY = imageHeight
	}
	if maxX <= x || maxY <= y {
		return image.Rectangle{}, fmt.Errorf("snapshot crop is outside image bounds")
	}
	return image.Rect(x, y, maxX, maxY), nil
}
